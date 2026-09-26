package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
	"github.com/nekoimi/scrapio/internal/repo/resource_repo"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
	"github.com/nekoimi/scrapio/internal/script"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

const (
	workflowTaskType = "workflow"
	workflowLease    = 5 * time.Minute
	workflowPoll     = 500 * time.Millisecond
)

// Worker executes persisted workflow root tasks. It deliberately lives beside
// the DSL rather than in internal/crawler, keeping provider-specific workers
// and the generic workflow runtime independently deployable.
type Worker struct {
	browser *drission_rod.DrissionRod
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	count   int
}

func NewWorker() *Worker { return &Worker{} }

func (w *Worker) Name() string { return "WorkflowWorker" }

func (w *Worker) Start(parent context.Context) error {
	if _, err := task_repo.RecoverExpiredLeases(); err != nil {
		return fmt.Errorf("recover expired task leases: %w", err)
	}
	cfg := bean.PtrFromContext[config.Config](parent)
	w.browser = bean.PtrFromContext[drission_rod.DrissionRod](parent)
	w.count = 1
	if cfg != nil && cfg.Crawler != nil && cfg.Crawler.WorkerNum > 0 {
		w.count = cfg.Crawler.WorkerNum
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := task_repo.RecoverExpiredLeases(); err != nil {
					log.Warnf("恢复过期任务租约失败: %s", err)
				}
			}
		}
	}()
	for i := 0; i < w.count; i++ {
		w.wg.Add(1)
		go w.loop(ctx, i)
	}
	return nil
}

func (w *Worker) Stop(_ context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	return nil
}

func (w *Worker) loop(ctx context.Context, index int) {
	defer w.wg.Done()
	workerID := fmt.Sprintf("workflow-%d-%s", index, uuid.NewString()[:8])
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claim, found, err := task_repo.ClaimNextType(workerID, workflowLease, workflowTaskType)
		if err != nil {
			if !task_repo.DatabaseUnavailable() {
				log.Warnf("领取 workflow 任务失败: %s", err)
			}
			if !waitWorkflow(ctx, workflowPoll) {
				return
			}
			continue
		}
		if !found {
			if !waitWorkflow(ctx, workflowPoll) {
				return
			}
			continue
		}
		taskCtx, stopLease := task_repo.MaintainLease(ctx, claim.Task.Id, claim.Attempt, workflowLease)
		execErr := w.execute(taskCtx, claim)
		stopLease()
		if err := execErr; err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, task_repo.ErrStaleAttempt) {
				continue
			}
			retryable := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
			var browserErr *drission_rod.BrowserError
			if errors.As(err, &browserErr) {
				retryable = browserErr.Retryable
			}
			var fetchErr *FetchError
			if errors.As(err, &fetchErr) {
				retryable = fetchErr.Retryable
			}
			if _, failErr := task_repo.Fail(claim.Task.Id, claim.Attempt.Id, err, retryable); failErr != nil && !errors.Is(failErr, task_repo.ErrStaleAttempt) {
				log.Errorf("回写 workflow 失败状态异常: %s", failErr)
			}
			continue
		}
	}
}

func waitWorkflow(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (w *Worker) execute(ctx context.Context, claim *task_repo.Claim) (execErr error) {
	run := new(table.WorkflowRun)
	has, err := db.Instance().ID(claim.Task.RunId).Get(run)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return errors.New("workflow run not found")
	}
	version := new(table.WorkflowVersion)
	has, err = db.Instance().ID(run.WorkflowVersionId).Get(version)
	if err != nil || !has {
		if err != nil {
			return err
		}
		return errors.New("workflow version not found")
	}
	definition, err := ParseExecutableDefinition(version.Definition)
	if err != nil {
		return fmt.Errorf("published workflow cannot execute: %w", err)
	}
	_, deadline, err := task_repo.InitializeRunBudget(run.Id)
	if err != nil {
		return fmt.Errorf("initialize run budget: %w", err)
	}
	parentCtx := ctx
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	defer func() {
		var exceeded *task_repo.BudgetExceeded
		if errors.As(execErr, &exceeded) {
			execErr = task_repo.CompleteLimited(claim, definition.Trigger.URL, exceeded.Reason)
		} else if execErr != nil && parentCtx.Err() == nil && !time.Now().Before(deadline) && !errors.Is(execErr, task_repo.ErrStaleAttempt) {
			execErr = task_repo.CompleteLimited(claim, definition.Trigger.URL, "max_duration_seconds")
		}
	}()
	input := map[string]any{}
	if strings.TrimSpace(claim.Task.Input) != "" {
		if err := json.Unmarshal([]byte(claim.Task.Input), &input); err != nil {
			return fmt.Errorf("parse task input: %w", err)
		}
	}
	pageURL := stringValue(input["url"])
	if pageURL == "" {
		pageURL = definition.Trigger.URL
	}
	for _, node := range append(append([]Node{}, definition.Acquire...), definition.Nodes...) {
		if strings.EqualFold(node.Type, "navigate") && stringValue(node.Config["url"]) != "" {
			pageURL = stringValue(node.Config["url"])
			break
		}
	}
	if pageURL == "" {
		return errors.New("workflow entry url is required")
	}
	if err := task_repo.ReservePage(claim, pageURL); err != nil {
		return fmt.Errorf("reserve page: %w", err)
	}
	fetchOptions := definition.FetchForRole(claim.Task.StepName)
	var result FetchResult
	var documentID int64
	// Resume from the immutable document if a worker died after fetching or
	// writing records. Idempotency keys then see the same candidates on replay.
	if definition.Persistence == "records" && claim.Task.OutputDocumentId != nil {
		var stored table.Document
		has, err := db.Instance().ID(*claim.Task.OutputDocumentId).Where("task_id=?", claim.Task.Id).Get(&stored)
		if err != nil {
			return err
		}
		if !has {
			return errors.New("task resume document not found")
		}
		result = FetchResult{RequestedURL: pageURL, FinalURL: pageURL, Adapter: "replay", ContentType: stored.DocumentType}
		var metadata struct {
			FinalURL string `json:"final_url"`
		}
		_ = json.Unmarshal([]byte(stored.Metadata), &metadata)
		if metadata.FinalURL != "" {
			result.FinalURL = metadata.FinalURL
		}
		if stored.DocumentType == "json" {
			result.JSON = stored.Content
		} else {
			result.HTML = stored.Content
		}
		documentID = stored.Id
	} else {
		result, err = (Fetcher{Browser: w.browser, AllowURL: func(raw string) error { return task_repo.CheckPageURL(claim, raw) }}).Fetch(ctx, pageURL, fetchOptions)
		if err != nil {
			var fetchError *FetchError
			if errors.As(err, &fetchError) {
				return err
			}
			return &StageError{Stage: "fetch", Cause: err}
		}
	}
	if definition.Listing != nil {
		entry, _ := url.Parse(definition.Trigger.URL)
		final, parseErr := url.Parse(result.FinalURL)
		if parseErr != nil || !strings.EqualFold(entry.Hostname(), final.Hostname()) {
			return fmt.Errorf("listing fetch redirected outside entry host: %s", result.FinalURL)
		}
	}
	pageURL = result.FinalURL
	if documentID == 0 {
		documentID, err = task_repo.SaveAttemptDocument(claim, func(s *xorm.Session) (int64, error) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			return saveDocument(s, claim.Task.Id, pageURL, result)
		})
		if err != nil {
			return &StageError{Stage: "capture", Cause: err}
		}
	}
	if definition.Persistence == "records" {
		if definition.Listing != nil && claim.Task.StepName != "detail" {
			if err := w.expandListing(ctx, claim, run, definition, result, documentID, input); err != nil {
				return &StageError{Stage: "discover", Cause: err}
			}
			return nil
		}
		return w.persistRecords(ctx, claim, run, definition, result, documentID)
	}

	values := map[string]any{}
	discoveredURLs := map[string]struct{}{}
	for _, node := range append(append([]Node{}, definition.Acquire...), definition.Nodes...) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !nodeApplies(node, claim.Task.StepName) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(node.Type)) {
		case "extract":
			extracted, err := extractNodeValues(node, result)
			if err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
			for key, value := range extracted {
				values[key] = value
			}
		case "discover":
			if claim.Task.StepName != "trigger" {
				continue
			}
			extracted, err := extractNodeValues(node, result)
			if err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
			fieldName := stringValue(node.Config["url_field"])
			if fieldName == "" {
				fieldName = firstMapKey(extracted)
			}
			for _, rawURL := range stringSlice(extracted[fieldName]) {
				childURL, err := resolveURL(pageURL, rawURL)
				if err != nil || childURL == "" || childURL == pageURL {
					continue
				}
				if _, exists := discoveredURLs[childURL]; exists {
					continue
				}
				discoveredURLs[childURL] = struct{}{}
				if err := task_repo.CheckAttempt(claim.Task.Id, claim.Attempt); err != nil {
					return err
				}
				if _, err := task_repo.CreateTaskForAttempt(claim, "detail", task_repo.TaskInput(childURL, "")); err != nil {
					var exceeded *task_repo.BudgetExceeded
					if errors.As(err, &exceeded) {
						delete(discoveredURLs, childURL)
						continue
					}
					return fmt.Errorf("create discovered task: %w", err)
				}
			}
		case "transform":
			if err := ApplyTransform(values, node.Config); err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
		case "validate":
			if err := ValidateValues(values, node.Config); err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
		case "script":
			timeout := 2 * time.Second
			if rawTimeout, ok := node.Config["timeout_ms"].(float64); ok && rawTimeout > 0 {
				timeout = time.Duration(rawTimeout) * time.Millisecond
			}
			scriptResult, err := script.Execute(ctx, script.Request{
				Script: stringValue(node.Config["script"]), Input: values, Timeout: timeout,
			})
			if err != nil {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
			output, ok := scriptResult.Output.(map[string]any)
			if !ok {
				return fmt.Errorf("node %s script output must be an object", node.Name)
			}
			for key, value := range output {
				values[key] = value
			}
		}
	}
	var resourceID int64
	if len(discoveredURLs) == 0 && claim.Task.StepName == "trigger" && len(values) == 0 {
		return errors.New("workflow produced no discovered pages or extracted fields")
	}
	err = task_repo.WithActiveAttempt(claim.Task.Id, claim.Attempt, func() error {
		var writeErr error
		resourceID, writeErr = persistResource(values, pageURL, run.WorkflowId)
		return writeErr
	})
	if err != nil {
		return err
	}
	if resourceID > 0 {
		if _, err := record_repo.ObserveLegacyWorkflowResource(resourceID, run.WorkflowVersionId, run.Id, claim.Task.Id, documentID); err != nil {
			return fmt.Errorf("write workflow record observation: %w", err)
		}
	}
	if resourceID == 0 && len(discoveredURLs) == 0 {
		return errors.New("workflow produced no persisted resource or discovered pages")
	}
	output := map[string]any{"document_id": documentID, "values": values, "discovered_count": len(discoveredURLs),
		"fetch": map[string]any{"adapter": result.Adapter, "final_url": result.FinalURL, "status_code": result.StatusCode, "content_type": result.ContentType, "action_results": result.Actions}}
	if resourceID > 0 {
		output["resource_id"] = resourceID
	} else {
		output["resource_persisted"] = false
	}
	encoded, _ := json.Marshal(output)
	return task_repo.Complete(claim.Task.Id, claim.Attempt.Id, string(encoded))
}

func (w *Worker) expandListing(ctx context.Context, claim *task_repo.Claim, run *table.WorkflowRun, definition Definition, document FetchResult, documentID int64, input map[string]any) error {
	pageIndex := 1
	if claim.Task.StepName == "list" {
		pageIndex = intNumber(input["page_index"])
		if pageIndex < 2 || pageIndex > definition.Listing.MaxPages {
			return errors.New("invalid listing page index")
		}
	}
	listing, err := ExtractListing(*definition.Listing, document, definition.Trigger.URL)
	if err != nil {
		return err
	}
	if len(listing.Details) > 1000 {
		if err := task_repo.RecordLimit(run.Id, claim.Task.Id, "detail", document.FinalURL, "max_discovered_per_page"); err != nil {
			return err
		}
		listing.Details = listing.Details[:1000]
	}
	newDetails := 0
	for _, detailURL := range listing.Details {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := task_repo.CheckAttempt(claim.Task.Id, claim.Attempt); err != nil {
			return err
		}
		existing, has, err := task_repo.WorkflowTaskByURL(run.Id, "detail", detailURL)
		if err != nil {
			return err
		}
		if has {
			if existing.ParentTaskId != nil && *existing.ParentTaskId == claim.Task.Id {
				newDetails++
			}
			continue
		}
		if _, err := task_repo.CreateTaskForAttempt(claim, "detail", task_repo.TaskInput(detailURL, "")); err != nil {
			var exceeded *task_repo.BudgetExceeded
			if errors.As(err, &exceeded) {
				continue
			}
			return err
		}
		newDetails++
	}
	emptyStreak := 0
	if newDetails == 0 && claim.Task.StepName == "list" {
		emptyStreak = intNumber(input["empty_streak"]) + 1
	} else if newDetails == 0 {
		emptyStreak = 1
	}
	entry, _ := url.Parse(definition.Trigger.URL)
	repeated := listing.NextURL == listingNormalized(entry) || listing.NextURL == document.FinalURL
	if listing.NextURL != "" && !repeated {
		existing, has, err := task_repo.WorkflowTaskByURL(run.Id, "list", listing.NextURL)
		if err != nil {
			return err
		}
		// A child created by this same attempt's replay is not a pagination loop.
		repeated = has && (existing.ParentTaskId == nil || *existing.ParentTaskId != claim.Task.Id)
	}
	stop := listingStop(*definition.Listing, pageIndex, emptyStreak, listing.NextURL, repeated)
	if stop == "" {
		childInput, _ := json.Marshal(map[string]any{"url": listing.NextURL, "page_index": pageIndex + 1, "empty_streak": emptyStreak})
		if err := task_repo.CheckAttempt(claim.Task.Id, claim.Attempt); err != nil {
			return err
		}
		if _, err := task_repo.CreateTaskForAttempt(claim, "list", string(childInput)); err != nil {
			var exceeded *task_repo.BudgetExceeded
			if errors.As(err, &exceeded) {
				stop = exceeded.Reason
			} else {
				return err
			}
		}
	}
	if stop == "max_pages" {
		if err := task_repo.RecordLimit(run.Id, claim.Task.Id, "list", listing.NextURL, "max_pages"); err != nil {
			return err
		}
	}
	output, _ := json.Marshal(map[string]any{"document_id": documentID, "page_role": "list", "page_index": pageIndex, "discovered_count": newDetails, "next_url": listing.NextURL, "stop_reason": stop, "limited": stop == "max_pages", "fetch": map[string]any{"adapter": document.Adapter, "final_url": document.FinalURL, "status_code": document.StatusCode}})
	return task_repo.Complete(claim.Task.Id, claim.Attempt.Id, string(output))
}

func intNumber(value any) int {
	number, _ := value.(float64)
	return int(number)
}

func (w *Worker) persistRecords(ctx context.Context, claim *task_repo.Claim, run *table.WorkflowRun, definition Definition, document FetchResult, documentID int64) error {
	owner := new(table.Workflow)
	has, err := db.Instance().ID(run.WorkflowId).Get(owner)
	if err != nil {
		return err
	}
	if !has || owner.DatasetId == nil {
		return errors.New("workflow dataset not found")
	}
	schema, err := record_repo.DatasetSchema(*owner.DatasetId)
	if err != nil {
		return err
	}
	values, err := RecordCandidates(definition, document, claim.Task.StepName, schema)
	if err != nil {
		return &StageError{Stage: "extract", Cause: err}
	}
	candidates := make([]record_repo.Candidate, 0, len(values))
	for i, value := range values {
		candidates = append(candidates, record_repo.Candidate{DatasetID: *owner.DatasetId, ExpectedSchemaVersion: schema.Version, Values: value, SourceID: &owner.SourceId, SourceURL: document.FinalURL, WorkflowID: &run.WorkflowId, WorkflowVersionID: &run.WorkflowVersionId, RunID: &run.Id, TaskID: &claim.Task.Id, DocumentID: &documentID, IdempotencyKey: fmt.Sprintf("workflow-task:%d:item:%d", claim.Task.Id, i)})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var results []record_repo.Result
	err = task_repo.WithActiveAttempt(claim.Task.Id, claim.Attempt, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var writeErr error
		results, writeErr = record_repo.SaveBatch(candidates)
		return writeErr
	})
	if err != nil {
		return &StageError{Stage: "persist", Cause: err, CandidateCount: len(candidates)}
	}
	output, _ := json.Marshal(map[string]any{"document_id": documentID, "record_count": len(results), "record_results": results, "fetch": map[string]any{"adapter": document.Adapter, "final_url": document.FinalURL, "status_code": document.StatusCode}})
	return task_repo.Complete(claim.Task.Id, claim.Attempt.Id, string(output))
}

func fieldRules(raw any) ([]FieldRule, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var fields []FieldRule
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("invalid extract.fields: %w", err)
	}
	return fields, nil
}

func extractNodeValues(node Node, result FetchResult) (map[string]any, error) {
	fields, err := fieldRules(node.Config["fields"])
	if err != nil {
		return nil, err
	}
	content, contentType := result.HTML, "html"
	if strings.EqualFold(stringValue(node.Config["content_type"]), "json") || (content == "" && result.JSON != "") || usesJSONPath(fields) {
		content, contentType = result.JSON, "json"
	}
	if content == "" {
		return nil, errors.New("fetch returned no extractable document")
	}
	return Extract(ExtractRequest{ContentType: contentType, Content: content, Fields: fields})
}

func firstMapKey(values map[string]any) string {
	for _, preferred := range []string{"urls", "url", "links", "link"} {
		if _, exists := values[preferred]; exists {
			return preferred
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		return key
	}
	return ""
}

func resolveURL(baseURL, rawURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	child, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || child.String() == "" {
		return "", err
	}
	resolved := base.ResolveReference(child)
	if !strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https") {
		return "", fmt.Errorf("unsupported discovered URL scheme: %s", resolved.Scheme)
	}
	if resolved.Host == "" {
		return "", errors.New("discovered URL host is empty")
	}
	return resolved.String(), nil
}

func usesJSONPath(fields []FieldRule) bool {
	for _, field := range fields {
		if strings.EqualFold(field.SelectorType, "jsonpath") || strings.EqualFold(field.SelectorType, "json_path") || strings.HasPrefix(strings.TrimSpace(field.Selector), "$") {
			return true
		}
	}
	return false
}

func nodeApplies(node Node, step string) bool {
	raw, ok := node.Config["run_on"]
	if !ok || raw == nil {
		return true
	}
	step = strings.ToLower(strings.TrimSpace(step))
	switch value := raw.(type) {
	case string:
		return strings.EqualFold(value, step)
	case []any:
		for _, item := range value {
			if strings.EqualFold(stringValue(item), step) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func saveDocument(s *xorm.Session, taskID int64, pageURL string, result FetchResult) (int64, error) {
	if db.Instance() == nil {
		return 0, errors.New("database is not initialized")
	}
	content, documentType := result.HTML, "html"
	if content == "" {
		content, documentType = result.JSON, "json"
	}
	if content == "" {
		return 0, errors.New("fetch returned empty document")
	}
	hash := sha256.Sum256([]byte(content))
	metadata, _ := json.Marshal(map[string]any{"url": pageURL, "requested_url": result.RequestedURL, "final_url": result.FinalURL, "adapter": result.Adapter, "status_code": result.StatusCode, "content_type": result.ContentType, "request_id": result.RequestID, "duration_ms": result.Duration.Milliseconds(), "action_results": result.Actions})
	document := &table.Document{TaskId: &taskID, DocumentType: documentType, Content: content, ContentHash: hex.EncodeToString(hash[:]), ContentSize: int64(len(content)), Metadata: string(metadata), CreatedAt: time.Now()}
	if _, err := s.InsertOne(document); err != nil {
		return 0, err
	}
	if len(result.Screenshot) > 0 && len(result.Screenshot) <= 10*1024*1024 {
		assetHash := sha256.Sum256(result.Screenshot)
		_, err := s.Exec(`INSERT INTO document_assets (document_id, asset_type, content_type, content, content_hash, content_size, metadata) VALUES (?, 'screenshot', 'image/png', ?, ?, ?, '{}'::jsonb) ON CONFLICT (document_id, asset_type) DO UPDATE SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash, content_size = EXCLUDED.content_size`, document.Id, result.Screenshot, hex.EncodeToString(assetHash[:]), len(result.Screenshot))
		if err != nil {
			return document.Id, err
		}
	}
	return document.Id, nil
}

func persistResource(values map[string]any, pageURL string, workflowID int64) (int64, error) {
	title := stringValue(values["title"])
	number := stringValue(values["number"])
	if number == "" {
		number = stringValue(values["canonical_key"])
	}
	links := stringSlice(values["links"])
	optimal := stringValue(values["optimal_link"])
	if optimal == "" {
		optimal = stringValue(values["optimalLink"])
	}
	if number == "" && len(links) == 0 && optimal == "" {
		return 0, nil
	}
	workflow := new(table.Workflow)
	has, err := db.Instance().ID(workflowID).Get(workflow)
	if err != nil || !has {
		if err != nil {
			return 0, err
		}
		return 0, errors.New("workflow not found")
	}
	source, ok := resource_repo.GetSource(workflow.SourceId)
	if !ok {
		return 0, errors.New("workflow source not found")
	}
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed == nil || parsed.Host == "" {
		return 0, errors.New("workflow entry url is invalid")
	}
	actress := stringValue(values["actress"])
	if actress == "" {
		actress = stringValue(values["actress0"])
	}
	resource, err := resource_repo.SaveCollected(source.Code, title, number, actress, parsed.Host, parsed.RequestURI(), links, optimal)
	if err != nil {
		return 0, err
	}
	return resource.Id, nil
}

func stringValue(value any) string {
	switch item := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(item)
	case json.Number:
		return item.String()
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(item))
	}
}

func stringSlice(value any) []string {
	switch item := value.(type) {
	case []any:
		result := make([]string, 0, len(item))
		for _, value := range item {
			if text := stringValue(value); text != "" {
				result = append(result, text)
			}
		}
		return result
	case []string:
		return item
	case string:
		if strings.TrimSpace(item) == "" {
			return nil
		}
		return strings.Fields(item)
	default:
		return nil
	}
}
