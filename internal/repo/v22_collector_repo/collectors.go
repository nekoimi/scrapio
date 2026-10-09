package v22_collector_repo

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
)

type CreateInput struct {
	Name      string `json:"name"`
	EntryURL  string `json:"entry_url"`
	EntryType string `json:"entry_type"`
}

var ErrIdempotencyConflict = errors.New("idempotency key was already used for another collector request")

type UpdateInput struct {
	Name             string          `json:"name"`
	ExpectedRevision int             `json:"expected_revision"`
	Definition       json.RawMessage `json:"definition"`
}

type Collector struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	EntryURL         string          `json:"entry_url"`
	EntryType        string          `json:"entry_type"`
	Status           string          `json:"status"`
	Definition       json.RawMessage `json:"definition"`
	Revision         int             `json:"revision"`
	ValidationStatus string          `json:"validation_status"`
	ValidationErrors []string        `json:"validation_errors"`
	SaveSummary      *SaveSummary    `json:"save_summary,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type SaveSummary struct {
	Revision      int       `json:"revision"`
	ChangedFields []string  `json:"changed_fields"`
	SavedAt       time.Time `json:"saved_at"`
}

type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type ListPage struct {
	Items      []table.V22Collector
	NextCursor string
	HasMore    bool
}

func ToDTO(row *table.V22Collector) Collector {
	status, errors := validationState(row)
	return Collector{ID: strconv.FormatInt(row.Id, 10), Name: row.Name, EntryURL: row.EntryURL, EntryType: row.EntryType, Status: row.Status, Definition: json.RawMessage(row.Definition), Revision: row.Revision, ValidationStatus: status, ValidationErrors: errors, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func validationState(row *table.V22Collector) (string, []string) {
	if row.ValidatedRevision == 0 {
		return "not_validated", []string{}
	}
	var result ValidationResult
	if json.Unmarshal([]byte(row.ValidationSummary), &result) != nil {
		return "stale", []string{}
	}
	if row.ValidatedRevision != row.Revision {
		return "stale", result.Errors
	}
	if !result.Valid {
		return "invalid", result.Errors
	}
	return "valid", []string{}
}

func validateDefinition(raw json.RawMessage, entryURL string) ValidationResult {
	var definition map[string]any
	if err := json.Unmarshal(raw, &definition); err != nil || definition == nil {
		return ValidationResult{Errors: []string{"definition must be a JSON object"}}
	}
	if version, ok := definition["definition_version"].(float64); !ok || version != 1 {
		return ValidationResult{Errors: []string{"definition_version must be 1"}}
	}
	value, ok := definition["entry_url"].(string)
	if !ok || strings.TrimSpace(value) != strings.TrimSpace(entryURL) {
		return ValidationResult{Errors: []string{"definition.entry_url must match collector entry_url"}}
	}
	if _, err := url.ParseRequestURI(entryURL); err != nil {
		return ValidationResult{Errors: []string{"entry_url must be an absolute http(s) URL"}}
	}
	parsed, _ := url.Parse(entryURL)
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ValidationResult{Errors: []string{"entry_url must be an absolute http(s) URL"}}
	}
	if _, ok := definition["steps"].([]any); !ok {
		return ValidationResult{Errors: []string{"definition.steps must be an array"}}
	}
	seen := map[string]bool{}
	if raw, exists := definition["http_request"]; exists {
		data, _ := json.Marshal(raw)
		if _, err := capture.DecodeRequest(data); err != nil {
			return ValidationResult{Errors: []string{"http_request must use bounded GET/POST and credential references"}}
		}
	}
	for _, raw := range definition["steps"].([]any) {
		step, ok := raw.(map[string]any)
		if !ok {
			return ValidationResult{Errors: []string{"steps must contain objects"}}
		}
		id, validID := step["step_id"].(string)
		if !validID || id == "" || seen[id] {
			return ValidationResult{Errors: []string{"step_id is required and unique"}}
		}
		seen[id] = true
		if step["type"] == "record_set" {
			if _, err := editor.RecordPlanFromStep(step); err != nil {
				return ValidationResult{Errors: []string{err.Error()}}
			}
		}
		if step["type"] == "json_records" {
			data, _ := json.Marshal(step["config"])
			if _, err := capture.DecodePlan(data); err != nil {
				return ValidationResult{Errors: []string{"json_records requires an array pointer, fields and bounded record count"}}
			}
		}
	}
	return ValidationResult{Valid: true, Errors: []string{}}
}

type RevisionConflict struct {
	Latest *table.V22Collector
}

func (e *RevisionConflict) Error() string { return "draft revision conflict" }

func validateCreate(input CreateInput) error {
	if _, err := capture.ValidateURL(input.EntryURL); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(input.EntryURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("entry_url must be an absolute http(s) URL")
	}
	if input.EntryType != "web" && input.EntryType != "json" {
		return errors.New("entry_type must be web or json")
	}
	if len([]rune(strings.TrimSpace(input.Name))) > 160 {
		return errors.New("name is too long")
	}
	if len(input.EntryURL) > 2048 {
		return errors.New("entry_url is too long")
	}
	return nil
}

func validateIdempotencyKey(key string) error {
	if len([]rune(key)) > 128 {
		return errors.New("Idempotency-Key exceeds 128 characters")
	}
	return nil
}

func Create(ownerID int64, input CreateInput, idempotencyKey string) (*table.V22Collector, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return nil, err
	}
	if ownerID <= 0 || db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	input.Name, input.EntryURL = strings.TrimSpace(input.Name), strings.TrimSpace(input.EntryURL)
	if input.Name == "" {
		if u, err := url.Parse(input.EntryURL); err == nil {
			input.Name = u.Host
		}
	}
	if err := validateCreate(input); err != nil {
		return nil, err
	}
	definition, _ := json.Marshal(map[string]any{"definition_version": 1, "entry_url": input.EntryURL, "steps": []any{}})
	now := time.Now()
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "collector.create", idempotencyKey); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		var idem table.V22IdempotencyKey
		if has, err := s.Where("owner_id = ? AND operation = ? AND key = ?", ownerID, "collector.create", idempotencyKey).Get(&idem); err != nil {
			return nil, err
		} else if has {
			var prior struct {
				Input *CreateInput `json:"input"`
			}
			if err := json.Unmarshal([]byte(idem.Response), &prior); err != nil {
				return nil, err
			}
			if prior.Input != nil && *prior.Input != input {
				return nil, ErrIdempotencyConflict
			}
			row := new(table.V22Collector)
			if has, err := s.Where("id = ? AND owner_id = ?", idem.ResourceId, ownerID).Get(row); err != nil || !has {
				return nil, fmt.Errorf("idempotent collector not found")
			}
			return row, nil
		}
	}
	row := &table.V22Collector{OwnerId: ownerID, Name: input.Name, EntryURL: input.EntryURL, EntryType: input.EntryType, Status: "draft", Definition: string(definition), Revision: 1, ValidationSummary: `{"valid":false,"errors":[]}`, CreatedBy: ownerID, UpdatedBy: ownerID, CreatedAt: now, UpdatedAt: now}
	if _, err := s.InsertOne(row); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		response, _ := json.Marshal(map[string]any{"id": row.Id, "input": input})
		if _, err := s.InsertOne(&table.V22IdempotencyKey{OwnerId: ownerID, Operation: "collector.create", Key: idempotencyKey, ResourceId: row.Id, Response: string(response), CreatedAt: now}); err != nil {
			return nil, err
		}
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func Get(ownerID, id int64) (*table.V22Collector, bool, error) {
	if ownerID <= 0 || id <= 0 || db.Instance() == nil {
		return nil, false, errors.New("collector id is required")
	}
	row := new(table.V22Collector)
	has, err := db.Instance().Where("id = ? AND owner_id = ?", id, ownerID).Get(row)
	return row, has, err
}

func Copy(ownerID, id int64, idempotencyKey string) (*table.V22Collector, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return nil, err
	}
	if ownerID <= 0 || db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "collector.copy", idempotencyKey); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		var idem table.V22IdempotencyKey
		if has, err := s.Where("owner_id = ? AND operation = ? AND key = ?", ownerID, "collector.copy", idempotencyKey).Get(&idem); err != nil {
			return nil, err
		} else if has {
			var prior struct {
				SourceID int64 `json:"source_id"`
			}
			if err := json.Unmarshal([]byte(idem.Response), &prior); err != nil {
				return nil, err
			}
			if prior.SourceID != 0 && prior.SourceID != id {
				return nil, ErrIdempotencyConflict
			}
			row := new(table.V22Collector)
			if has, err := s.Where("id = ? AND owner_id = ?", idem.ResourceId, ownerID).Get(row); err != nil || !has {
				return nil, errors.New("idempotent collector not found")
			}
			return row, nil
		}
	}
	original := new(table.V22Collector)
	if has, err := s.Where("id = ? AND owner_id = ? AND status <> ?", id, ownerID, "archived").Get(original); err != nil {
		return nil, err
	} else if !has {
		return nil, errors.New("collector not found")
	}
	now := time.Now()
	copyName := []rune(original.Name)
	const suffix = " (副本)"
	if len(copyName)+len([]rune(suffix)) > 160 {
		copyName = copyName[:160-len([]rune(suffix))]
	}
	copy := &table.V22Collector{
		OwnerId: ownerID, Name: string(copyName) + suffix, EntryURL: original.EntryURL,
		EntryType: original.EntryType, Status: "draft", Definition: original.Definition,
		Revision: 1, ValidationSummary: `{"valid":false,"errors":[]}`, CreatedBy: ownerID, UpdatedBy: ownerID, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.InsertOne(copy); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		response, _ := json.Marshal(map[string]any{"id": copy.Id, "source_id": id})
		if _, err := s.InsertOne(&table.V22IdempotencyKey{OwnerId: ownerID, Operation: "collector.copy", Key: idempotencyKey, ResourceId: copy.Id, Response: string(response), CreatedAt: now}); err != nil {
			return nil, err
		}
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return copy, nil
}

func encodeCursor(row table.V22Collector) string {
	payload := row.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(row.Id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(cursor string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, errors.New("invalid cursor")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return time.Time{}, 0, errors.New("invalid cursor")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, errors.New("invalid cursor")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return time.Time{}, 0, errors.New("invalid cursor")
	}
	return updatedAt, id, nil
}

func List(ownerID int64, limit int, cursor string) (ListPage, error) {
	if ownerID <= 0 || db.Instance() == nil {
		return ListPage{}, errors.New("database is not initialized")
	}
	if limit < 1 || limit > 100 {
		return ListPage{}, errors.New("limit must be between 1 and 100")
	}
	rows := make([]table.V22Collector, 0)
	query := db.Instance().Where("owner_id = ? AND status <> ?", ownerID, "archived")
	if cursor != "" {
		updatedAt, id, err := decodeCursor(cursor)
		if err != nil {
			return ListPage{}, err
		}
		query = query.And("(updated_at < ? OR (updated_at = ? AND id < ?))", updatedAt, updatedAt, id)
	}
	err := query.Desc("updated_at").Desc("id").Limit(limit + 1).Find(&rows)
	if err != nil {
		return ListPage{}, err
	}
	page := ListPage{Items: rows}
	if len(rows) > limit {
		page.HasMore = true
		page.Items = rows[:limit]
		page.NextCursor = encodeCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func Update(ownerID, id int64, input UpdateInput) (*table.V22Collector, []string, error) {
	if db.Instance() == nil {
		return nil, nil, errors.New("database is not initialized")
	}
	if input.ExpectedRevision <= 0 {
		return nil, nil, errors.New("expected_revision is required")
	}
	if len(input.Definition) == 0 || !json.Valid(input.Definition) {
		return nil, nil, errors.New("definition must be valid JSON")
	}
	var definitionObject map[string]any
	if err := json.Unmarshal(input.Definition, &definitionObject); err != nil || definitionObject == nil {
		return nil, nil, errors.New("definition must be a JSON object")
	}
	if raw, exists := definitionObject["http_request"]; exists {
		data, _ := json.Marshal(raw)
		if _, err := capture.DecodeRequest(data); err != nil {
			return nil, nil, err
		}
	}
	entryURL := ""
	hasEntryURL := false
	if value, exists := definitionObject["entry_url"]; exists {
		hasEntryURL = true
		var valid bool
		entryURL, valid = value.(string)
		if !valid {
			return nil, nil, errors.New("definition.entry_url must be a string")
		}
		entryURL = strings.TrimSpace(entryURL)
	}
	if strings.TrimSpace(input.Name) != "" && len([]rune(strings.TrimSpace(input.Name))) > 160 {
		return nil, nil, errors.New("name is too long")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? AND owner_id = ? FOR UPDATE", id, ownerID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("collector not found")
	}
	row := new(table.V22Collector)
	if has, err := s.ID(id).Get(row); err != nil || !has {
		return nil, nil, errors.New("collector not found")
	}
	if row.Status == "archived" {
		return nil, nil, errors.New("collector not found")
	}
	if row.Revision != input.ExpectedRevision {
		return nil, nil, &RevisionConflict{Latest: row}
	}
	changed := make([]string, 0, 3)
	if strings.TrimSpace(input.Name) != "" {
		name := strings.TrimSpace(input.Name)
		if name != row.Name {
			changed = append(changed, "name")
		}
		row.Name = name
	}
	if hasEntryURL {
		if err := validateCreate(CreateInput{Name: row.Name, EntryURL: entryURL, EntryType: row.EntryType}); err != nil {
			return nil, nil, err
		}
		if entryURL != row.EntryURL {
			changed = append(changed, "entry_url")
		}
		row.EntryURL = entryURL
	}
	if string(input.Definition) != row.Definition {
		changed = append(changed, "definition")
	}
	if len(changed) == 0 {
		return row, changed, nil
	}
	row.Definition, row.Revision, row.UpdatedBy, row.UpdatedAt = string(input.Definition), row.Revision+1, ownerID, time.Now()
	if _, err := s.ID(id).Cols("name", "entry_url", "definition", "revision", "updated_by", "updated_at").Update(row); err != nil {
		return nil, nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, nil, err
	}
	return row, changed, nil
}

func Validate(ownerID, id int64) (*table.V22Collector, ValidationResult, error) {
	if ownerID <= 0 || id <= 0 || db.Instance() == nil {
		return nil, ValidationResult{}, errors.New("collector not found")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, ValidationResult{}, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? AND owner_id = ? FOR UPDATE", id, ownerID)
	if err != nil {
		return nil, ValidationResult{}, err
	}
	if len(rows) == 0 {
		return nil, ValidationResult{}, errors.New("collector not found")
	}
	row := new(table.V22Collector)
	if has, err := s.ID(id).Get(row); err != nil || !has {
		return nil, ValidationResult{}, errors.New("collector not found")
	}
	if row.Status == "archived" {
		return nil, ValidationResult{}, errors.New("collector not found")
	}
	result := validateDefinition(json.RawMessage(row.Definition), row.EntryURL)
	summary, _ := json.Marshal(result)
	row.ValidatedRevision, row.ValidationSummary = row.Revision, string(summary)
	if _, err := s.ID(id).Cols("validated_revision", "validation_summary").Update(row); err != nil {
		return nil, ValidationResult{}, err
	}
	if err := s.Commit(); err != nil {
		return nil, ValidationResult{}, err
	}
	return row, result, nil
}

func Archive(ownerID, id int64) error {
	row, has, err := Get(ownerID, id)
	if err != nil {
		return err
	}
	if !has {
		return errors.New("collector not found")
	}
	now := time.Now()
	_, err = db.Instance().Where("id = ? AND owner_id = ? AND status <> ?", row.Id, ownerID, "archived").Cols("status", "archived_at", "updated_at", "updated_by").Update(&table.V22Collector{Status: "archived", ArchivedAt: &now, UpdatedAt: now, UpdatedBy: ownerID})
	return err
}
