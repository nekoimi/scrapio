package record_repo

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/record"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
	"github.com/nekoimi/scrapio/internal/repo/plugin_repo"
	"xorm.io/xorm"
)

type Candidate struct {
	SuppressPlugins       bool // Explicit offline/backfill writes never dispatch external effects.
	DatasetID             int64
	ExpectedSchemaVersion int
	Values                map[string]any
	SourceID              *int64
	SourceURL             string
	WorkflowID            *int64
	WorkflowVersionID     *int64
	RunID                 *int64
	TaskID                *int64
	DocumentID            *int64
	IdempotencyKey        string
	LegacyResourceID      *int64
}
type Result struct {
	RecordID      int64    `json:"record_id"`
	ObservationID int64    `json:"observation_id"`
	Decision      string   `json:"decision"`
	CanonicalKey  string   `json:"canonical_key"`
	ChangedFields []string `json:"changed_fields"`
}

func DatasetSchema(id int64) (record.Schema, error) {
	dataset, fields, err := dataset_repo.Detail(id, 0)
	if err != nil {
		return record.Schema{}, err
	}
	if dataset.Status != "active" {
		return record.Schema{}, errors.New("active dataset not found")
	}
	schema := record.Schema{Version: dataset.SchemaVersion, EmptyValuePolicy: dataset.EmptyValuePolicy}
	if err := json.Unmarshal([]byte(dataset.UniqueKeyFields), &schema.UniqueKeyFields); err != nil {
		return schema, err
	}
	for _, field := range fields {
		schema.Fields = append(schema.Fields, record.Field{Key: field.FieldKey, Type: field.FieldType, Required: field.Required, Multiple: field.Multiple})
	}
	return schema, nil
}

// SaveBatch commits all candidates together; errors leave no partial batch.
func SaveBatch(candidates []Candidate) ([]Result, error) {
	if db.Instance() == nil {
		return nil, errors.New("database is not initialized")
	}
	if len(candidates) == 0 || len(candidates) > 1000 {
		return nil, errors.New("require 1–1000 candidates")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	results := make([]Result, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.DatasetID != candidates[0].DatasetID {
			return nil, errors.New("batch must target one dataset")
		}
		result, err := save(s, candidate)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func Save(c Candidate) (Result, error) {
	if db.Instance() == nil {
		return Result{}, errors.New("database is not initialized")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return Result{}, err
	}
	result, err := save(s, c)
	if err != nil {
		_ = s.Rollback()
		return result, err
	}
	if err := s.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func save(s *xorm.Session, c Candidate) (Result, error) {
	if c.DatasetID <= 0 {
		return Result{}, errors.New("dataset id is required")
	}
	// Serialize writes per dataset, including schema edits and competing keys.
	rows, err := s.QueryString("SELECT id FROM datasets WHERE id = ? AND status = 'active' FOR UPDATE", c.DatasetID)
	if err != nil {
		return Result{}, err
	}
	if len(rows) == 0 {
		return Result{}, errors.New("active dataset not found")
	}
	dataset := new(table.Dataset)
	if _, err := s.ID(c.DatasetID).Get(dataset); err != nil {
		return Result{}, err
	}
	if c.ExpectedSchemaVersion > 0 && dataset.SchemaVersion != c.ExpectedSchemaVersion {
		return Result{}, errors.New("dataset schema changed during extraction; retry with current schema")
	}
	var fields []table.DatasetField
	if err := s.Where("dataset_id = ? AND schema_version = ?", c.DatasetID, dataset.SchemaVersion).Asc("ordinal").Find(&fields); err != nil {
		return Result{}, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(dataset.UniqueKeyFields), &keys); err != nil {
		return Result{}, err
	}
	schema := record.Schema{Version: dataset.SchemaVersion, UniqueKeyFields: keys, EmptyValuePolicy: dataset.EmptyValuePolicy}
	for _, field := range fields {
		schema.Fields = append(schema.Fields, record.Field{Key: field.FieldKey, Type: field.FieldType, Required: field.Required, Multiple: field.Multiple})
	}
	values := c.Values
	if dataset.RecordType == "magnet" {
		values = make(map[string]any, len(c.Values))
		for key, value := range c.Values {
			values[key] = value
		}
		if key, ok := values["canonical_key"].(string); ok {
			values["canonical_key"] = strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return unicode.ToUpper(r)
			}, strings.TrimSpace(key))
		}
	}
	prepared, err := record.Prepare(schema, values)
	if err != nil {
		return Result{Decision: "invalid"}, err
	}
	if len(c.IdempotencyKey) > 256 {
		return Result{}, errors.New("idempotency key is too long")
	}
	if c.IdempotencyKey != "" {
		existing, err := s.QueryString("SELECT o.record_id, o.id, o.decision, o.normalized_fields::text AS normalized_fields, r.canonical_key FROM record_observations o JOIN records r ON r.id = o.record_id WHERE o.dataset_id = ? AND o.idempotency_key = ?", c.DatasetID, c.IdempotencyKey)
		if err != nil {
			return Result{}, err
		}
		if len(existing) > 0 {
			raw, _ := json.Marshal(prepared.Values)
			var prior any
			var current any
			_ = json.Unmarshal([]byte(existing[0]["normalized_fields"]), &prior)
			_ = json.Unmarshal(raw, &current)
			if existing[0]["canonical_key"] != prepared.Key || !reflect.DeepEqual(prior, current) {
				return Result{Decision: "conflict"}, errors.New("idempotency key was already used for a different candidate")
			}
			rid, _ := strconv.ParseInt(existing[0]["record_id"], 10, 64)
			oid, _ := strconv.ParseInt(existing[0]["id"], 10, 64)
			if c.LegacyResourceID != nil {
				if _, err := s.Exec("INSERT INTO legacy_resource_records (legacy_resource_id,record_id,observation_id) VALUES (?,?,?) ON CONFLICT (legacy_resource_id) DO UPDATE SET record_id = EXCLUDED.record_id, observation_id = EXCLUDED.observation_id, migrated_at = NOW()", *c.LegacyResourceID, rid, oid); err != nil {
					return Result{}, err
				}
			}
			return Result{RecordID: rid, ObservationID: oid, Decision: existing[0]["decision"], CanonicalKey: prepared.Key}, nil
		}
	}
	currentRows, err := s.QueryString("SELECT id, normalized::text AS normalized FROM records WHERE dataset_id = ? AND canonical_key = ?", c.DatasetID, prepared.Key)
	if err != nil {
		return Result{}, err
	}
	has := len(currentRows) > 0
	var current struct {
		Id         int64
		Normalized string
	}
	if has {
		current.Id, err = strconv.ParseInt(currentRows[0]["id"], 10, 64)
		if err != nil {
			return Result{}, err
		}
		current.Normalized = currentRows[0]["normalized"]
	}
	now := time.Now()
	decision := "created"
	merged := prepared.Values
	changed := []string{}
	if has {
		var previous map[string]any
		if err := json.Unmarshal([]byte(current.Normalized), &previous); err != nil {
			return Result{}, err
		}
		merged, changed = record.Merge(previous, prepared.Values, schema.EmptyValuePolicy)
		if len(changed) == 0 {
			decision = "unchanged"
		} else {
			decision = "updated"
		}
	}
	encoded, _ := json.Marshal(merged)
	raw, _ := json.Marshal(c.Values)
	normalized, _ := json.Marshal(prepared.Values)
	recordID := current.Id
	if !has {
		inserted, err := s.QueryString("INSERT INTO records (dataset_id,canonical_key,normalized,content_hash,first_seen_at,last_seen_at,created_at,updated_at) VALUES (?,?,?::jsonb,encode(digest((?::jsonb)::text,'sha256'),'hex'),?,?,?,?) RETURNING id", c.DatasetID, prepared.Key, string(encoded), string(encoded), now, now, now, now)
		if err != nil {
			return Result{}, err
		}
		recordID, err = strconv.ParseInt(inserted[0]["id"], 10, 64)
		if err != nil {
			return Result{}, err
		}
		for k := range merged {
			changed = append(changed, k)
		}
		sort.Strings(changed)
	} else if decision == "updated" {
		if _, err := s.Exec("UPDATE records SET normalized = ?::jsonb, content_hash = encode(digest((?::jsonb)::text,'sha256'),'hex'), last_seen_at = ?, updated_at = ? WHERE id = ?", string(encoded), string(encoded), now, now, recordID); err != nil {
			return Result{}, err
		}
	} else {
		if _, err := s.Exec("UPDATE records SET last_seen_at = ? WHERE id = ?", now, recordID); err != nil {
			return Result{}, err
		}
	}
	var idem any
	if strings.TrimSpace(c.IdempotencyKey) != "" {
		idem = c.IdempotencyKey
	}
	observation, err := s.QueryString(`INSERT INTO record_observations
 (record_id,dataset_id,schema_version,workflow_id,workflow_version_id,run_id,task_id,document_id,source_id,source_url,raw_fields,normalized_fields,decision,idempotency_key,legacy_resource_id,observed_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,?,?,?,?) RETURNING id`, recordID, c.DatasetID, dataset.SchemaVersion, c.WorkflowID, c.WorkflowVersionID, c.RunID, c.TaskID, c.DocumentID, c.SourceID, c.SourceURL, string(raw), string(normalized), decision, idem, c.LegacyResourceID, now)
	if err != nil {
		return Result{}, err
	}
	observationID, err := strconv.ParseInt(observation[0]["id"], 10, 64)
	if err != nil {
		return Result{}, err
	}
	if decision == "created" || decision == "updated" {
		before := "{}"
		if has {
			before = current.Normalized
		}
		changedJSON, _ := json.Marshal(changed)
		if _, err := s.Exec("INSERT INTO record_revisions (record_id,observation_id,before_fields,after_fields,changed_fields,created_at) VALUES (?,?,?::jsonb,?::jsonb,?::jsonb,?)", recordID, observationID, before, string(encoded), string(changedJSON), now); err != nil {
			return Result{}, err
		}
	}
	if c.LegacyResourceID != nil {
		if _, err := s.Exec("INSERT INTO legacy_resource_records (legacy_resource_id,record_id,observation_id) VALUES (?,?,?) ON CONFLICT (legacy_resource_id) DO UPDATE SET record_id = EXCLUDED.record_id, observation_id = EXCLUDED.observation_id, migrated_at = NOW()", *c.LegacyResourceID, recordID, observationID); err != nil {
			return Result{}, err
		}
	}
	if !c.SuppressPlugins && c.LegacyResourceID == nil && c.WorkflowID != nil && c.WorkflowVersionID != nil && c.RunID != nil && c.TaskID != nil && (decision == "created" || decision == "updated") {
		provenance := map[string]any{"dataset_id": c.DatasetID, "record_id": recordID, "observation_id": observationID, "workflow_id": c.WorkflowID, "workflow_version_id": c.WorkflowVersionID, "run_id": c.RunID, "task_id": c.TaskID, "document_id": c.DocumentID, "source_url": c.SourceURL}
		if err := plugin_repo.QueueRecordEvent(s, c.DatasetID, *c.WorkflowID, recordID, observationID, "record."+decision, merged, provenance); err != nil {
			return Result{}, err
		}
	}
	return Result{RecordID: recordID, ObservationID: observationID, Decision: decision, CanonicalKey: prepared.Key, ChangedFields: changed}, nil
}

func List(datasetID int64, page, size int) ([]table.Record, int64, error) {
	if db.Instance() == nil {
		return nil, 0, errors.New("database is not initialized")
	}
	if datasetID <= 0 {
		return nil, 0, errors.New("dataset id is required")
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	total, err := db.Instance().Where("dataset_id = ?", datasetID).Count(new(table.Record))
	if err != nil {
		return nil, 0, err
	}
	rows := make([]table.Record, 0)
	err = db.Instance().Where("dataset_id = ?", datasetID).Desc("last_seen_at", "id").Limit(size, (page-1)*size).Find(&rows)
	return rows, total, err
}

func Detail(id int64, limit int) (*table.Record, []table.RecordObservation, []table.RecordRevision, error) {
	if db.Instance() == nil {
		return nil, nil, nil, errors.New("database is not initialized")
	}
	if id <= 0 {
		return nil, nil, nil, errors.New("record id is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	row := new(table.Record)
	has, err := db.Instance().ID(id).Get(row)
	if err != nil {
		return nil, nil, nil, err
	}
	if !has {
		return nil, nil, nil, errors.New("record not found")
	}
	observations := make([]table.RecordObservation, 0)
	revisions := make([]table.RecordRevision, 0)
	if err := db.Instance().Where("record_id = ?", id).Desc("observed_at", "id").Limit(limit).Find(&observations); err != nil {
		return nil, nil, nil, err
	}
	if err := db.Instance().Where("record_id = ?", id).Desc("created_at", "id").Limit(limit).Find(&revisions); err != nil {
		return nil, nil, nil, err
	}
	return row, observations, revisions, nil
}
