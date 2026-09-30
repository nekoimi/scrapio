package v22_collector_repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
)

type CreateInput struct {
	Name      string `json:"name"`
	EntryURL  string `json:"entry_url"`
	EntryType string `json:"entry_type"`
}

type UpdateInput struct {
	Name             string          `json:"name"`
	ExpectedRevision int             `json:"expected_revision"`
	Definition       json.RawMessage `json:"definition"`
}

type Collector struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	EntryURL   string          `json:"entry_url"`
	EntryType  string          `json:"entry_type"`
	Status     string          `json:"status"`
	Definition json.RawMessage `json:"definition"`
	Revision   int             `json:"revision"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func ToDTO(row *table.V22Collector) Collector {
	return Collector{ID: strconv.FormatInt(row.Id, 10), Name: row.Name, EntryURL: row.EntryURL, EntryType: row.EntryType, Status: row.Status, Definition: json.RawMessage(row.Definition), Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

type RevisionConflict struct {
	Latest *table.V22Collector
}

func (e *RevisionConflict) Error() string { return "draft revision conflict" }

func validateCreate(input CreateInput) error {
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
	if idempotencyKey != "" {
		var idem table.V22IdempotencyKey
		if has, err := s.Where("owner_id = ? AND operation = ? AND key = ?", ownerID, "collector.create", idempotencyKey).Get(&idem); err != nil {
			return nil, err
		} else if has {
			row := new(table.V22Collector)
			if has, err := s.ID(idem.ResourceId).Get(row); err != nil || !has {
				return nil, fmt.Errorf("idempotent collector not found")
			}
			return row, nil
		}
	}
	row := &table.V22Collector{OwnerId: ownerID, Name: input.Name, EntryURL: input.EntryURL, EntryType: input.EntryType, Status: "draft", Definition: string(definition), Revision: 1, CreatedBy: ownerID, UpdatedBy: ownerID, CreatedAt: now, UpdatedAt: now}
	if _, err := s.InsertOne(row); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		response, _ := json.Marshal(map[string]any{"id": row.Id})
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
	if idempotencyKey != "" {
		var idem table.V22IdempotencyKey
		if has, err := s.Where("owner_id = ? AND operation = ? AND key = ?", ownerID, "collector.copy", idempotencyKey).Get(&idem); err != nil {
			return nil, err
		} else if has {
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
		Revision: 1, CreatedBy: ownerID, UpdatedBy: ownerID, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.InsertOne(copy); err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		response, _ := json.Marshal(map[string]any{"id": copy.Id})
		if _, err := s.InsertOne(&table.V22IdempotencyKey{OwnerId: ownerID, Operation: "collector.copy", Key: idempotencyKey, ResourceId: copy.Id, Response: string(response), CreatedAt: now}); err != nil {
			return nil, err
		}
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return copy, nil
}

func List(ownerID int64) ([]table.V22Collector, error) {
	if ownerID <= 0 || db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	rows := make([]table.V22Collector, 0)
	err := db.Instance().Where("owner_id = ? AND status <> ?", ownerID, "archived").Desc("updated_at").Desc("id").Find(&rows)
	return rows, err
}

func Update(ownerID, id int64, input UpdateInput) (*table.V22Collector, error) {
	if input.ExpectedRevision <= 0 {
		return nil, errors.New("expected_revision is required")
	}
	if len(input.Definition) == 0 || !json.Valid(input.Definition) {
		return nil, errors.New("definition must be valid JSON")
	}
	var definitionObject map[string]any
	if err := json.Unmarshal(input.Definition, &definitionObject); err != nil || definitionObject == nil {
		return nil, errors.New("definition must be a JSON object")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? AND owner_id = ? FOR UPDATE", id, ownerID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("collector not found")
	}
	row := new(table.V22Collector)
	if has, err := s.ID(id).Get(row); err != nil || !has {
		return nil, errors.New("collector not found")
	}
	if row.Revision != input.ExpectedRevision {
		return nil, &RevisionConflict{Latest: row}
	}
	if strings.TrimSpace(input.Name) != "" {
		row.Name = strings.TrimSpace(input.Name)
	}
	row.Definition, row.Revision, row.UpdatedBy, row.UpdatedAt = string(input.Definition), row.Revision+1, ownerID, time.Now()
	if _, err := s.ID(id).Cols("name", "definition", "revision", "updated_by", "updated_at").Update(row); err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
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
	_, err = db.Instance().ID(row.Id).Cols("status", "archived_at", "updated_at", "updated_by").Update(&table.V22Collector{Status: "archived", ArchivedAt: &now, UpdatedAt: now, UpdatedBy: ownerID})
	return err
}
