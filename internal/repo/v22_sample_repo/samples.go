package v22_sample_repo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_governance_repo"
	"github.com/nekoimi/scrapio/internal/sample"
	"xorm.io/xorm"
)

var (
	ErrNotFound  = errors.New("sample or collector not found")
	ErrConflict  = errors.New("sample revision or idempotency conflict")
	ErrProtected = errors.New("sample is protected; remove protection before deleting")
	ErrInvalid   = errors.New("invalid sample input")
)

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func canonical(raw []byte) string {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return ""
	}
	out, _ := json.Marshal(v)
	return string(out)
}
func ExpectedHash(raw string) string { return capture.Hash(sample.CanonicalJSON([]byte(raw))) }

type CreateInput struct {
	Name               string          `json:"name"`
	CaptureID          string          `json:"capture_id"`
	ExpectedRevision   int             `json:"expected_revision"`
	StepID             string          `json:"step_id"`
	Stage              string          `json:"stage"`
	Kind               string          `json:"kind"`
	Expected           json.RawMessage `json:"expected"`
	ExpectedJSON       *string         `json:"expected_json,omitempty"`
	Protected          bool            `json:"protected"`
	IncludeScreenshot  bool            `json:"include_screenshot"`
	ScreenshotReviewed bool            `json:"screenshot_reviewed"`
	Masks              []sample.Mask   `json:"masks"`
}

func (i *CreateInput) Validate() error {
	if i.ExpectedJSON != nil {
		if len(i.Expected) > 0 {
			return invalid("expected and expected_json are mutually exclusive")
		}
		i.Expected = json.RawMessage(*i.ExpectedJSON)
		i.ExpectedJSON = nil
	}
	i.Name = strings.TrimSpace(i.Name)
	if i.Name == "" || len([]rune(i.Name)) > 160 || i.ExpectedRevision < 1 || i.StepID == "" || len(i.StepID) > 128 || i.Stage != "list" && i.Stage != "detail" || i.Kind != "normal" && i.Kind != "missing_field" {
		return invalid("name, revision, step, list/detail stage and normal/missing_field kind required")
	}
	if _, err := uuid.Parse(i.CaptureID); err != nil {
		return invalid("invalid capture_id")
	}
	if _, err := sample.DecodeExpected(i.Expected); err != nil {
		return invalid(err.Error())
	}
	i.Expected = json.RawMessage(canonical(i.Expected))
	if err := sample.ValidateMasks(i.Masks); err != nil {
		return invalid(err.Error())
	}
	if i.IncludeScreenshot && !i.ScreenshotReviewed || !i.IncludeScreenshot && (i.ScreenshotReviewed || len(i.Masks) > 0) {
		return invalid("screenshot must be explicitly reviewed; masks belong to included screenshots")
	}
	return nil
}
func Fingerprint(collectorID int64, i CreateInput) string {
	raw, _ := json.Marshal([]any{collectorID, i})
	return capture.Hash(raw)
}
func validateKey(key string) error {
	if key == "" || len([]rune(key)) > 128 {
		return invalid("Idempotency-Key is required and limited to 128 characters")
	}
	return nil
}
func Existing(ownerID, collectorID int64, key string, input CreateInput) (*table.V22Sample, error) {
	row := new(table.V22Sample)
	has, err := db.Instance().Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	if row.CollectorId != collectorID || row.Fingerprint != Fingerprint(collectorID, input) {
		return nil, ErrConflict
	}
	return row, nil
}
func GetByKey(ownerID int64, key string) (*table.V22Sample, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	row := new(table.V22Sample)
	has, err := db.Instance().Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func Get(ownerID int64, id string) (*table.V22Sample, error) {
	row := new(table.V22Sample)
	has, err := db.Instance().Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}

func loadCollector(s *xorm.Session, ownerID, id int64, revision int) (*table.V22Collector, error) {
	if _, err := s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", id, ownerID); err != nil {
		return nil, err
	}
	row := new(table.V22Collector)
	has, err := s.Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has || row.Status == "archived" {
		return nil, ErrNotFound
	}
	if row.Revision != revision {
		return nil, &v22_collector_repo.RevisionConflict{Latest: row}
	}
	return row, nil
}
func loadSample(s *xorm.Session, ownerID int64, id string, revision int) (*table.V22Sample, error) {
	if _, err := s.QueryString("SELECT id FROM v22_samples WHERE id=? AND owner_id=? FOR UPDATE", id, ownerID); err != nil {
		return nil, err
	}
	row := new(table.V22Sample)
	has, err := s.Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if row.Revision != revision {
		return nil, ErrConflict
	}
	return row, nil
}
func snapshot(s *xorm.Session, ownerID, collectorID int64, id string) (*table.V22Capture, error) {
	if _, err := s.QueryString("SELECT id FROM v22_captures WHERE id=? AND owner_id=? FOR SHARE", id, ownerID); err != nil {
		return nil, err
	}
	row := new(table.V22Capture)
	has, err := s.Where("id=? AND owner_id=? AND collector_id=?", id, ownerID, collectorID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if row.Status != "succeeded" || row.ContentHash != capture.Hash([]byte(row.Content)) {
		return nil, invalid("a successful intact capture is required")
	}
	return row, nil
}
func planFor(collector *table.V22Collector, capture *table.V22Capture, step, stage string) (extraction.Plan, error) {
	plan, err := extraction.PlanFromDefinition([]byte(collector.Definition), step)
	if err != nil {
		return plan, invalid(err.Error())
	}
	if plan.Kind == "json_records" && (capture.Format != "json" || stage != "list") || plan.Kind == "record_set" && (capture.Format != "html" || stage == "detail" && plan.HTML.Detail == nil) {
		return plan, invalid("capture format or role does not match the extraction step")
	}
	return plan, nil
}

// Only receipt metadata is copied. Input strings, credentials, URLs, arbitrary
// error text and request payloads are deliberately absent from sample evidence.
type ActionSummary struct {
	CommandID string    `json:"command_id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Before    string    `json:"before_page_state_id"`
	After     string    `json:"after_page_state_id"`
	ErrorCode string    `json:"error_code"`
	CreatedAt time.Time `json:"created_at"`
}

func actionSummaries(s *xorm.Session, ownerID int64, c *table.V22Capture) (string, bool, error) {
	if c.SessionId == "" {
		return "[]", false, nil
	}
	rows := []table.V22EditorCommand{}
	err := s.Where("owner_id=? AND collector_id=? AND session_id=? AND created_at<=? AND updated_at<=? AND status IN ('succeeded','failed')", ownerID, c.CollectorId, c.SessionId, c.CreatedAt, c.CreatedAt).Desc("created_at", "id").Limit(51).Find(&rows)
	if err != nil {
		return "", false, err
	}
	truncated := len(rows) > 50
	if truncated {
		rows = rows[:50]
	}
	out := []ActionSummary{}
	for index := len(rows) - 1; index >= 0; index-- {
		r := rows[index]
		var request struct {
			Type string `json:"type"`
		}
		var result struct {
			Before    string `json:"before_page_state_id"`
			After     string `json:"after_page_state_id"`
			ErrorCode string `json:"error_code"`
		}
		_ = json.Unmarshal([]byte(r.Request), &request)
		_ = json.Unmarshal([]byte(r.Result), &result)
		out = append(out, ActionSummary{r.Id, request.Type, r.Status, result.Before, result.After, result.ErrorCode, r.CreatedAt})
	}
	raw, _ := json.Marshal(out)
	return string(raw), truncated, nil
}
func Create(ownerID, collectorID int64, key string, i CreateInput, screenshot []byte) (*table.V22Sample, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "sample.create", key); err != nil {
		return nil, err
	}
	previous := new(table.V22Sample)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(previous)
	if err != nil {
		return nil, err
	}
	if has {
		if previous.Fingerprint != Fingerprint(collectorID, i) {
			return nil, ErrConflict
		}
		return previous, nil
	}
	collector, err := loadCollector(s, ownerID, collectorID, i.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	count, err := s.Where("owner_id=? AND collector_id=?", ownerID, collectorID).Count(new(table.V22Sample))
	if err != nil {
		return nil, err
	}
	if count >= 100 {
		return nil, invalid("collector sample limit (100) reached")
	}
	if err := v22_governance_repo.ReserveAssets(s, ownerID, int64(len(screenshot))); err != nil {
		return nil, err
	}
	source, err := snapshot(s, ownerID, collectorID, i.CaptureID)
	if err != nil {
		return nil, err
	}
	if _, err = planFor(collector, source, i.StepID, i.Stage); err != nil {
		return nil, err
	}
	if i.IncludeScreenshot && (source.Source != "browser" || len(screenshot) == 0) || !i.IncludeScreenshot && len(screenshot) > 0 {
		return nil, invalid("screenshot must come from the bound browser capture")
	}
	actions, truncated, err := actionSummaries(s, ownerID, source)
	if err != nil {
		return nil, err
	}
	masks, _ := json.Marshal(i.Masks)
	if i.Masks == nil {
		masks = []byte("[]")
	}
	policy := "excluded"
	imageHash := ""
	if i.IncludeScreenshot {
		policy = "user-reviewed-masked-png.v1"
		imageHash = capture.Hash(screenshot)
	}
	now := time.Now()
	row := &table.V22Sample{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, CaptureId: source.Id, Name: i.Name, Stage: i.Stage, StepId: i.StepID, Kind: i.Kind, Revision: 1, SavedRevision: collector.Revision, DefinitionHash: capture.Hash([]byte(collector.Definition)), Expected: string(i.Expected), ExpectedHash: ExpectedHash(string(i.Expected)), Actions: actions, ActionsTruncated: truncated, Protected: i.Protected, Screenshot: screenshot, ScreenshotHash: imageHash, Masks: string(masks), ScreenshotPolicy: policy, IdempotencyKey: key, Fingerprint: Fingerprint(collectorID, i), CreatedAt: now, UpdatedAt: now}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

type UpdateInput struct {
	ExpectedRevision int             `json:"expected_revision"`
	Name             *string         `json:"name,omitempty"`
	Expected         json.RawMessage `json:"expected,omitempty"`
	ExpectedJSON     *string         `json:"expected_json,omitempty"`
	Protected        *bool           `json:"protected,omitempty"`
}

func Update(ownerID int64, id string, i UpdateInput) (*table.V22Sample, error) {
	if i.ExpectedJSON != nil {
		if len(i.Expected) > 0 {
			return nil, invalid("expected and expected_json are mutually exclusive")
		}
		i.Expected = json.RawMessage(*i.ExpectedJSON)
	}
	if i.ExpectedRevision < 1 || i.Name == nil && len(i.Expected) == 0 && i.Protected == nil {
		return nil, invalid("sample revision and changes required")
	}
	if i.Name != nil {
		*i.Name = strings.TrimSpace(*i.Name)
		if *i.Name == "" || len([]rune(*i.Name)) > 160 {
			return nil, invalid("sample name is empty or too long")
		}
	}
	if len(i.Expected) > 0 {
		if _, err := sample.DecodeExpected(i.Expected); err != nil {
			return nil, invalid(err.Error())
		}
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	row, err := loadSample(s, ownerID, id, i.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	if i.Name != nil {
		row.Name = *i.Name
	}
	if len(i.Expected) > 0 {
		row.Expected = canonical(i.Expected)
		row.ExpectedHash = ExpectedHash(row.Expected)
	}
	if i.Protected != nil {
		row.Protected = *i.Protected
	}
	row.Revision++
	row.UpdatedAt = time.Now()
	if _, err = s.ID(id).Cols("name", "expected", "expected_hash", "protected", "revision", "updated_at").Update(row); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Delete(ownerID int64, id string, revision int) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	row, err := loadSample(s, ownerID, id, revision)
	if err != nil {
		return err
	}
	if row.Protected {
		return ErrProtected
	}
	if _, err = s.ID(id).Delete(new(table.V22Sample)); err != nil {
		return err
	}
	return s.Commit()
}

type cursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func List(ownerID, collectorID int64, limit int, token string) ([]table.V22Sample, string, error) {
	if limit < 1 || limit > 50 {
		return nil, "", invalid("limit must be 1..50")
	}
	s := db.Instance().Where("owner_id=? AND collector_id=?", ownerID, collectorID).Omit("screenshot")
	defer s.Close()
	if token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(token)
		var c cursor
		if err != nil || json.Unmarshal(raw, &c) != nil || c.At.IsZero() {
			return nil, "", invalid("invalid cursor")
		}
		if _, err = uuid.Parse(c.ID); err != nil {
			return nil, "", invalid("invalid cursor")
		}
		s.And("(created_at < ? OR (created_at = ? AND id < ?))", c.At, c.At, c.ID)
	}
	rows := []table.V22Sample{}
	if err := s.Desc("created_at", "id").Limit(limit + 1).Find(&rows); err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		raw, _ := json.Marshal(cursor{last.CreatedAt, last.Id})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return rows, next, nil
}

type CheckInput struct {
	ExpectedRevision int `json:"expected_revision"`
	SampleRevision   int `json:"sample_revision"`
}

func Check(ctx context.Context, ownerID int64, id, key string, i CheckInput) (*table.V22SampleCheck, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if i.ExpectedRevision < 1 || i.SampleRevision < 1 {
		return nil, invalid("collector and sample revision required")
	}
	fingerprintRaw, _ := json.Marshal([]any{id, i})
	fingerprint := capture.Hash(fingerprintRaw)
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "sample.check", key); err != nil {
		return nil, err
	}
	previous := new(table.V22SampleCheck)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(previous)
	if err != nil {
		return nil, err
	}
	if has {
		if previous.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return previous, nil
	}
	sampleRow, err := loadSample(s, ownerID, id, i.SampleRevision)
	if err != nil {
		return nil, err
	}
	collector, err := loadCollector(s, ownerID, sampleRow.CollectorId, i.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	source, err := snapshot(s, ownerID, collector.Id, sampleRow.CaptureId)
	if err != nil {
		return nil, err
	}
	expected, err := sample.DecodeExpected([]byte(sampleRow.Expected))
	if err != nil || ExpectedHash(sampleRow.Expected) != sampleRow.ExpectedHash {
		return nil, invalid("sample expectations are corrupt")
	}
	// Invalid/newly removed rules are saved as failed evidence as well.
	plan, planErr := planFor(collector, source, sampleRow.StepId, sampleRow.Stage)
	result := extraction.Result{Interpreter: extraction.InterpreterVersion, Stage: sampleRow.Stage, StepID: sampleRow.StepId, Records: []extraction.Record{}, Warnings: []string{}, DryRun: true}
	runErr := planErr
	if runErr == nil {
		result, runErr = extraction.Extract(ctx, extraction.Input{Content: source.Content, Format: source.Format, URL: source.FinalURL, BaseURL: source.BaseURL, Stage: sampleRow.Stage}, plan)
	}
	comparison := sample.Compare(result, expected)
	errorCode := ""
	errorMessage := ""
	if runErr != nil {
		errorCode = "EXTRACTION_FAILED"
		errorMessage = runErr.Error()
		if len(errorMessage) > 512 {
			errorMessage = errorMessage[:512]
		}
		comparison = sample.Comparison{Status: "failed", Differences: []sample.Difference{{Code: errorCode}}}
	}
	rawResult, _ := json.Marshal(result)
	rawComparison, _ := json.Marshal(comparison)
	row := &table.V22SampleCheck{Id: uuid.NewString(), OwnerId: ownerID, SampleId: id, SampleRevision: sampleRow.Revision, CollectorRevision: collector.Revision, DefinitionHash: capture.Hash([]byte(collector.Definition)), Definition: collector.Definition, ContentHash: source.ContentHash, ExpectedHash: sampleRow.ExpectedHash, Expected: sampleRow.Expected, InterpreterVersion: extraction.InterpreterVersion, Status: comparison.Status, Result: string(rawResult), Comparison: string(rawComparison), ErrorCode: errorCode, ErrorMessage: errorMessage, IdempotencyKey: key, Fingerprint: fingerprint, CreatedAt: time.Now()}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	// Bounded evidence history: never keep unbounded multi-MiB preview results.
	if _, err = s.Exec("DELETE FROM v22_sample_checks WHERE sample_id=? AND owner_id=? AND id NOT IN (SELECT id FROM v22_sample_checks WHERE sample_id=? AND owner_id=? ORDER BY created_at DESC,id DESC LIMIT 20)", id, ownerID, id, ownerID); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Checks(ownerID int64, id string) ([]table.V22SampleCheck, error) {
	if _, err := Get(ownerID, id); err != nil {
		return nil, err
	}
	rows := []table.V22SampleCheck{}
	err := db.Instance().Where("sample_id=? AND owner_id=?", id, ownerID).Omit("result", "expected", "comparison", "definition").Desc("created_at", "id").Limit(20).Find(&rows)
	return rows, err
}
func GetCheck(ownerID int64, sampleID, id string) (*table.V22SampleCheck, error) {
	row := new(table.V22SampleCheck)
	has, err := db.Instance().Where("id=? AND sample_id=? AND owner_id=?", id, sampleID, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
