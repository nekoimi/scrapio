package trial

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
)

type Document struct {
	ID            string            `json:"document_id"`
	StepID        string            `json:"step_id"`
	Stage         string            `json:"stage"`
	ParentIndex   int               `json:"parent_record_index"`
	ListPage      int               `json:"list_page"`
	CaptureID     string            `json:"capture_id,omitempty"`
	URL           string            `json:"source_url"`
	BaseURL       string            `json:"base_url"`
	Format        string            `json:"format"`
	Content       string            `json:"-"`
	ContentHash   string            `json:"content_hash"`
	Result        extraction.Result `json:"extraction"`
	OutputIndices *[]int            `json:"output_indices,omitempty"`
	LoopStop      string            `json:"loop_stop,omitempty"`
}
type FixedDocument struct {
	CaptureID string `json:"capture_id"`
	Content   string `json:"content"`
	Hash      string `json:"content_hash"`
	URL       string `json:"source_url"`
	BaseURL   string `json:"base_url"`
	Format    string `json:"format"`
}

// Source is owned by one trial; adapters never reuse the editing session.
type Source interface {
	Start(context.Context) error
	Snapshot(context.Context) (FixedDocument, error)
	Action(context.Context, editor.Command) error
	Close()
}
type ActionDiagnostic struct {
	ID   int    `json:"operation_id"`
	Type string `json:"type"`
}

type Event struct {
	Action     *ActionDiagnostic `json:"action,omitempty"`
	StepID     string            `json:"step_id,omitempty"`
	Stage      string            `json:"stage,omitempty"`
	Status     string            `json:"status"`
	Code       string            `json:"code,omitempty"`
	Checkpoint *Checkpoint       `json:"checkpoint,omitempty"`
}
type Summary struct {
	Status           string          `json:"status"`
	Reason           string          `json:"stop_reason"`
	FailedStep       string          `json:"failed_step_id"`
	FailedStage      string          `json:"failed_stage"`
	Pages            int             `json:"pages"`
	ListPages        int             `json:"list_pages,omitempty"`
	Details          int             `json:"details,omitempty"`
	DuplicateRecords int             `json:"duplicate_records,omitempty"`
	DuplicateDetails int             `json:"duplicate_details,omitempty"`
	LastCheckpoint   *Checkpoint     `json:"last_checkpoint,omitempty"`
	Candidates       int             `json:"candidates"`
	NetworkAccessed  bool            `json:"network_accessed"`
	DryRun           bool            `json:"dry_run"`
	Warnings         []string        `json:"warnings"`
	Output           *output.Preview `json:"output,omitempty"`
}
type Runner struct {
	Continuous bool
	Plan       Plan
	Input      Input
	Fixed      map[string]FixedDocument
	Source     Source
	Schema     output.Schema
	Existing   func(context.Context, extraction.Result) (map[string]output.Existing, error)
	Emit       func(Event, *Document) error
}

func (r Runner) Run(ctx context.Context) (summary Summary) {
	summary = Summary{Status: "running", DryRun: true, Warnings: []string{}}
	defer func() {
		seen := map[string]bool{}
		warnings := []string{}
		for _, warning := range summary.Warnings {
			if !seen[warning] {
				seen[warning] = true
				warnings = append(warnings, warning)
			}
		}
		summary.Warnings = warnings
	}()
	selected := extraction.Result{Records: []extraction.Record{}, Interpreter: extraction.InterpreterVersion, DryRun: true, Stage: r.Plan.Output.Stage, StepID: r.Plan.Output.StepID}
	currentStep, currentStage := "", "entry"
	limited := false
	invalid := false
	details := 0
	size := 0
	seenOutput := map[string]bool{}
	seenDetails := map[string]bool{}
	seenPages := map[string]bool{}
	seenURLs := map[string]bool{}
	var lastDocument *Document
	loopReason := ""
	setLimited := func(code string) {
		limited = true
		summary.Warnings = append(summary.Warnings, code)
		loopReason = code
	}
	fail := func(err error) Summary {
		summary.FailedStep = currentStep
		summary.FailedStage = currentStage
		summary.Reason = errorCode(err)
		summary.Status = "failed"
		if summary.Pages > 0 {
			summary.Status = "partial"
		}
		for _, code := range []string{"PAGE_BUDGET_REACHED", "RECORD_BUDGET_REACHED", "RESULT_BUDGET_REACHED", "OUTPUT_BUDGET_REACHED"} {
			if summary.Reason == code {
				summary.Status = "limited"
			}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			summary.Status = "cancelled"
			summary.Reason = "CANCEL_REQUESTED"
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			summary.Status = "limited"
			summary.Reason = "TIME_BUDGET_REACHED"
		}
		return summary
	}
	emit := func(status, code string, doc *Document) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.Emit != nil {
			return r.Emit(Event{StepID: currentStep, Stage: currentStage, Status: status, Code: code}, doc)
		}
		return nil
	}
	actionSequence := 0
	executeAction := func(cmd editor.Command) error {
		actionSequence++
		a := &ActionDiagnostic{ID: actionSequence, Type: cmd.Type}
		if r.Emit != nil {
			if err := r.Emit(Event{StepID: currentStep, Stage: currentStage, Status: "running", Action: a}, nil); err != nil {
				return err
			}
		}
		// No command values/selectors are journaled here; values may contain credentials.
		if err := ctx.Err(); err != nil {
			return err
		}
		err := r.Source.Action(ctx, cmd)
		status, code := "succeeded", ""
		if err != nil {
			status, code = "failed", errorCode(err)
			if code == "ACTION_OUTCOME_UNCERTAIN" {
				status = "unknown"
			}
		}
		if r.Emit != nil {
			if journalErr := r.Emit(Event{StepID: currentStep, Stage: currentStage, Status: status, Code: code, Action: a}, nil); journalErr != nil && err == nil {
				return journalErr
			}
		}
		return err
	}
	if r.Input.Mode == "live" {
		if r.Source == nil {
			return fail(errors.New("SOURCE_UNAVAILABLE"))
		}
		defer r.Source.Close()
		summary.NetworkAccessed = true
		if err := emit("running", "", nil); err != nil {
			return fail(err)
		}
		if err := r.Source.Start(ctx); err != nil {
			return fail(err)
		}
	}
	take := func(step Step, stage string, parent, page int) (FixedDocument, error) {
		currentStage = stage
		if summary.Pages >= r.Input.Budget.Pages {
			return FixedDocument{}, errors.New("PAGE_BUDGET_REACHED")
		}
		if err := emit("running", "", nil); err != nil {
			return FixedDocument{}, err
		}
		var fixed FixedDocument
		var err error
		if r.Input.Mode == "offline" {
			var has bool
			fixed, has = r.Fixed[step.ID+":"+stage]
			if !has {
				return fixed, errors.New("OFFLINE_INPUT_MISSING")
			}
		} else {
			fixed, err = r.Source.Snapshot(ctx)
			if err != nil {
				return fixed, err
			}
		}
		if fixed.Hash != capture.Hash([]byte(fixed.Content)) {
			return fixed, errors.New("INPUT_HASH_MISMATCH")
		}
		if r.Input.Mode == "live" && !Allowed(fixed.URL, r.Input.Origins) {
			return fixed, errors.New("ORIGIN_OUT_OF_SCOPE")
		}
		p := step.Plan
		eligible := step.ID == r.Plan.Output.StepID && stage == r.Plan.Output.Stage
		if eligible && !r.Continuous {
			remaining := r.Input.Budget.Records - len(selected.Records)
			if remaining < 1 {
				return fixed, errors.New("RECORD_BUDGET_REACHED")
			}
			if p.Kind == "json_records" && p.JSON.MaxRecords > remaining {
				p.JSON.MaxRecords = remaining
			}
			if p.Kind == "record_set" && p.HTML.MaxRecords > remaining {
				p.HTML.MaxRecords = remaining
			}
		}
		result, err := extraction.Extract(ctx, extraction.Input{Content: fixed.Content, Format: fixed.Format, URL: fixed.URL, BaseURL: fixed.BaseURL, Stage: stage}, p)
		if err != nil {
			return fixed, err
		}
		doc := &Document{StepID: step.ID, Stage: stage, ParentIndex: parent, ListPage: page, CaptureID: fixed.CaptureID, URL: fixed.URL, BaseURL: fixed.BaseURL, Format: fixed.Format, Content: fixed.Content, ContentHash: fixed.Hash, Result: result}
		if r.Continuous && stage == "list" && step.Plan.HTML.NextPage != nil {
			paths, e := extraction.DetailPaths(ctx, extraction.Input{Content: fixed.Content, Format: fixed.Format, URL: fixed.URL, BaseURL: fixed.BaseURL}, step.Plan)
			if e != nil {
				return fixed, e
			}
			fingerprint := step.ID + ":" + PageFingerprint(result, paths)
			if len(result.Records) == 0 && page > 1 {
				doc.LoopStop = "EMPTY_PAGE"
			} else if seenPages[fingerprint] {
				doc.LoopStop = "REPEATED_PAGE"
			}
			seenPages[fingerprint] = true
		}
		if r.Continuous && eligible {
			indices, duplicates := OutputIndices(result, r.Schema, r.Plan.Output.Mapping, seenOutput)
			summary.DuplicateRecords += duplicates
			remaining := r.Input.Budget.Records - len(selected.Records)
			if len(indices) > remaining {
				indices = indices[:remaining]
				doc.Result.Truncated = true
				result.Truncated = true
				setLimited("RECORD_BUDGET_REACHED")
			}
			if doc.LoopStop != "" {
				indices = []int{}
			}
			doc.OutputIndices = &indices
		}
		data, _ := json.Marshal(doc)
		size += len(data) + len(fixed.Content)
		if size > 8*capture.MaxBytes {
			return fixed, errors.New("RESULT_BUDGET_REACHED")
		}
		if err = emit("succeeded", "", doc); err != nil {
			return fixed, err
		}
		summary.Pages++
		lastDocument = doc
		if stage == "list" {
			summary.ListPages++
		} else {
			summary.Details++
		}
		if err := ctx.Err(); err != nil {
			return fixed, err
		}
		if result.InvalidCount > 0 || len(result.Records) == 0 && doc.LoopStop != "EMPTY_PAGE" {
			invalid = true
		}
		if result.Truncated {
			limited = true
			summary.Warnings = append(summary.Warnings, "EXTRACTION_TRUNCATED")
		}
		if eligible {
			for _, record := range result.Records {
				if doc.OutputIndices != nil {
					keep := false
					for _, index := range *doc.OutputIndices {
						if index == record.Index {
							keep = true
							break
						}
					}
					if !keep {
						continue
					}
				}
				record.Index = len(selected.Records)
				selected.Records = append(selected.Records, record)
			}
			selected.Truncated = selected.Truncated || result.Truncated
			summary.Candidates = len(selected.Records)
		}
		return fixed, nil
	}
	for _, step := range r.Plan.Steps {
		currentStep, currentStage = step.ID, "action"
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := emit("running", "", nil); err != nil {
			return fail(err)
		}
		if step.Kind == "action" || step.Kind == "navigate" {
			if r.Input.Mode == "offline" {
				limited = true
				summary.Warnings = append(summary.Warnings, "OFFLINE_ACTION_NOT_EXECUTED")
				if err := emit("skipped", "OFFLINE_ACTION_NOT_EXECUTED", nil); err != nil {
					return fail(err)
				}
				continue
			}
			if err := executeAction(step.Action); err != nil {
				return fail(err)
			}
			if err := emit("succeeded", "", nil); err != nil {
				return fail(err)
			}
			continue
		}
		maxPages := 1
		if step.Plan.HTML.NextPage != nil {
			maxPages = step.Plan.HTML.NextPage.MaxPages
			if !r.Continuous && maxPages > 2 {
				maxPages = 2
			}
		}
		checkpoint := func(doc *Document, state, next, reason string) error {
			if !r.Continuous || doc == nil || r.Emit == nil {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			cp := &Checkpoint{StepID: step.ID, ListPage: doc.ListPage, DocumentID: doc.ID, URL: doc.URL, Hash: doc.ContentHash, State: state, Next: next, Reason: reason, Pages: summary.Pages, ListPages: summary.ListPages, Candidates: summary.Candidates, Details: details, DuplicateRecords: summary.DuplicateRecords}
			if err := r.Emit(Event{StepID: step.ID, Stage: "checkpoint", Status: "running", Code: reason, Checkpoint: cp}, nil); err != nil {
				return err
			}
			summary.LastCheckpoint = cp
			return nil
		}
		for page := 1; page <= maxPages; page++ {
			priorCandidates := len(selected.Records)
			fixed, err := take(step, "list", -1, page)
			if err != nil {
				return fail(err)
			}
			listDoc := lastDocument
			seenURLs[step.ID+":"+URLKey(fixed.URL)] = true
			if listDoc.LoopStop != "" {
				if listDoc.LoopStop == "EMPTY_PAGE" {
					loopReason = "EMPTY_PAGE"
				} else {
					setLimited(listDoc.LoopStop)
				}
				if err = checkpoint(listDoc, "stopped", "", listDoc.LoopStop); err != nil {
					return fail(err)
				}
				break
			}
			if err = checkpoint(listDoc, "page_captured", "", ""); err != nil {
				return fail(err)
			}
			if step.Plan.HTML.Detail != nil {
				if r.Input.Mode == "offline" {
					if _, has := r.Fixed[step.ID+":detail"]; has {
						if _, err = take(step, "detail", -1, page); err != nil {
							return fail(err)
						}
					}
					limited = true
					summary.Warnings = append(summary.Warnings, "OFFLINE_DETAIL_PATH_NOT_EXECUTED")
				} else {
					paths, err := extraction.DetailPaths(ctx, extraction.Input{Content: fixed.Content, Format: fixed.Format, URL: fixed.URL, BaseURL: fixed.BaseURL, Stage: "list"}, step.Plan)
					if err != nil {
						return fail(err)
					}
					pageDetails := 0
					for _, path := range paths {
						currentStage = "detail-path"
						if path.Error != "" {
							return fail(errors.New(path.Error))
						}
						detailKey := step.ID + ":" + URLKey(path.URL)
						if r.Continuous && seenDetails[detailKey] {
							summary.DuplicateDetails++
							continue
						}
						if pageDetails >= step.Plan.HTML.Detail.MaxDetails || details >= r.Input.Budget.Details {
							limited = true
							summary.Warnings = append(summary.Warnings, "DETAIL_BUDGET_REACHED")
							break
						}
						if !Allowed(path.URL, r.Input.Origins) {
							return fail(errors.New("DETAIL_ORIGIN_OUT_OF_SCOPE"))
						}
						if err := ctx.Err(); err != nil {
							return fail(err)
						}
						if summary.Pages >= r.Input.Budget.Pages {
							if !r.Continuous {
								return fail(errors.New("PAGE_BUDGET_REACHED"))
							}
							setLimited("PAGE_BUDGET_REACHED")
							break
						}
						if step.ID == r.Plan.Output.StepID && r.Plan.Output.Stage == "detail" && len(selected.Records) >= r.Input.Budget.Records {
							if !r.Continuous {
								return fail(errors.New("RECORD_BUDGET_REACHED"))
							}
							setLimited("RECORD_BUDGET_REACHED")
							break
						}
						if err = checkpoint(listDoc, "action_pending", path.URL, "DETAIL_PATH"); err != nil {
							return fail(err)
						}
						cmd := editor.Command{Type: "navigate", Value: path.URL, TimeoutMS: 10000, Confirmed: true}
						if err = emit("running", "", nil); err != nil {
							return fail(err)
						}
						if err = executeAction(cmd); err != nil {
							return fail(err)
						}
						if _, err = take(step, "detail", path.Index, page); err != nil {
							return fail(err)
						}
						details++
						pageDetails++
						seenDetails[detailKey] = true
						if err = checkpoint(listDoc, "action_pending", fixed.URL, "RETURN_LIST"); err != nil {
							return fail(err)
						}
						currentStage = "return-list"
						cmd = editor.Command{Type: "back", TimeoutMS: 10000, Confirmed: true}
						if step.Plan.HTML.Detail.ReturnStrategy == "navigate-list" {
							cmd.Type = "navigate"
							cmd.Value = fixed.URL
						}
						if err := ctx.Err(); err != nil {
							return fail(err)
						}
						if err = emit("running", "", nil); err != nil {
							return fail(err)
						}
						if err = executeAction(cmd); err != nil {
							return fail(err)
						}
						if err = emit("running", "", nil); err != nil {
							return fail(err)
						}
						returned, err := r.Source.Snapshot(ctx)
						if err != nil {
							return fail(err)
						}
						same := returned.URL == fixed.URL
						if r.Continuous {
							same, err = SameList(ctx, fixed, returned, step.Plan)
							if err != nil {
								return fail(err)
							}
						}
						if !same {
							return fail(errors.New("RETURN_LIST_STATE_MISMATCH"))
						}
						if err = checkpoint(listDoc, "page_captured", "", "DETAIL_RETURNED"); err != nil {
							return fail(err)
						}
					}
				}
			}
			currentStage = "next-page"
			next := ""
			if step.Plan.HTML.NextPage != nil {
				if r.Input.Mode == "offline" {
					limited = true
					summary.Warnings = append(summary.Warnings, "OFFLINE_NEXT_PATH_NOT_EXECUTED")
					break
				}
				next, err = extraction.NextPath(ctx, extraction.Input{Content: fixed.Content, Format: fixed.Format, URL: fixed.URL, BaseURL: fixed.BaseURL}, step.Plan)
				if err != nil {
					return fail(err)
				}
			}
			reason := ""
			switch {
			case next == "":
				reason = "NO_NEXT_PAGE"
			case r.Continuous && page > 1 && step.ID == r.Plan.Output.StepID && len(selected.Records) == priorCandidates && !listDoc.Result.Truncated:
				reason = "NO_NEW_RECORDS"
				setLimited(reason)
			case next != "click" && seenURLs[step.ID+":"+URLKey(next)]:
				reason = "REPEATED_URL"
				setLimited(reason)
			case r.Continuous && r.Plan.Output.Stage == "detail" && step.ID == r.Plan.Output.StepID && details >= r.Input.Budget.Details:
				reason = "DETAIL_BUDGET_REACHED"
				setLimited(reason)
			case page >= maxPages:
				reason = "LIST_PAGE_LIMIT_REACHED"
				setLimited(reason)
			case summary.Pages >= r.Input.Budget.Pages:
				reason = "PAGE_BUDGET_REACHED"
				if !r.Continuous {
					return fail(errors.New(reason))
				}
				setLimited(reason)
			case len(selected.Records) >= r.Input.Budget.Records:
				reason = "RECORD_BUDGET_REACHED"
				setLimited(reason)
			}
			state := "page_complete"
			if reason != "" {
				state = "stopped"
				if loopReason == "" {
					loopReason = reason
				}
			}
			if err = checkpoint(listDoc, state, next, reason); err != nil {
				return fail(err)
			}
			if reason != "" {
				break
			}
			cmd := editor.Command{Type: "navigate", Value: next, TimeoutMS: 10000, Confirmed: true}
			if next == "click" {
				cmd.Type = "click"
				cmd.Value = ""
				cmd.Locator = step.Plan.HTML.NextPage.Locator
			} else if !Allowed(next, r.Input.Origins) {
				return fail(errors.New("NEXT_ORIGIN_OUT_OF_SCOPE"))
			}
			if err = checkpoint(listDoc, "action_pending", next, ""); err != nil {
				return fail(err)
			}
			if err = emit("running", "", nil); err != nil {
				return fail(err)
			}
			if err = executeAction(cmd); err != nil {
				return fail(err)
			}
			if wait := step.Plan.HTML.NextPage.WaitMS; wait > 0 {
				if err = executeAction(editor.Command{Type: "wait", Value: strconv.Itoa(wait), TimeoutMS: wait + 500, Confirmed: true}); err != nil {
					return fail(err)
				}
			}
			if err = checkpoint(listDoc, "awaiting_page", next, ""); err != nil {
				return fail(err)
			}

		}
	}
	currentStep, currentStage = r.Plan.Output.StepID, "output"
	if err := emit("running", "", nil); err != nil {
		return fail(err)
	}
	existing := map[string]output.Existing{}
	if r.Existing != nil {
		var err error
		existing, err = r.Existing(ctx, selected)
		if err != nil {
			return fail(err)
		}
	}
	var outputPlan extraction.Plan
	for _, s := range r.Plan.Steps {
		if s.ID == r.Plan.Output.StepID {
			outputPlan = s.Plan
		}
	}
	compat := output.CheckCompatibility(r.Schema, r.Plan.Output.Mapping, output.SourceFields(outputPlan, r.Plan.Output.Stage))
	preview := output.PreviewBatch(r.Schema, r.Plan.Output.Mapping, r.Plan.Output.UpdatePolicy, r.Plan.Output.EmptyPolicy, selected, compat, existing)
	data, _ := json.Marshal(preview)
	if len(data) > 2*capture.MaxBytes {
		return fail(errors.New("OUTPUT_BUDGET_REACHED"))
	}
	summary.Output = &preview
	summary.Status = "succeeded"
	summary.Reason = "COMPLETED"
	if invalid || !preview.Ready {
		summary.Status = "partial"
		summary.Reason = "VALIDATION_FAILED"
	}
	if limited {
		summary.Status = "limited"
		summary.Reason = "BOUNDED_OR_OFFLINE_PATHS"
	}
	if r.Continuous && loopReason != "" && (summary.Status == "succeeded" || summary.Status == "limited") {
		summary.Reason = loopReason
	}
	if err := emit(summary.Status, summary.Reason, nil); err != nil {
		return fail(err)
	}
	return summary
}
func errorCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "CANCEL_REQUESTED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIME_BUDGET_REACHED"
	}
	var coded interface{ TrialCode() string }
	if errors.As(err, &coded) {
		return coded.TrialCode()
	}
	// Only known interpreter/configuration failures reach here, never raw HTTP bodies.
	if len(err.Error()) > 240 {
		return "TRIAL_STEP_FAILED"
	}
	return err.Error()
}
