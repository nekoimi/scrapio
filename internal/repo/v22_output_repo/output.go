package v22_output_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"xorm.io/xorm"
)

var (
	ErrInvalid  = errors.New("invalid output configuration")
	ErrNotFound = errors.New("table, collector or input not found")
	ErrConflict = errors.New("output check is stale or request key conflicts")
	ErrBlocked  = errors.New("output check contains blocking issues")
)

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func validKey(key string) error {
	if key == "" || len([]rune(key)) > 128 {
		return invalid("Idempotency-Key required, maximum 128 characters")
	}
	return nil
}
func decode[T any](raw string) (T, error) {
	var value T
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&value)
	return value, err
}

type TableInput struct {
	Name   string        `json:"name"`
	Schema output.Schema `json:"schema"`
}

func (i *TableInput) Validate() error {
	i.Name = strings.TrimSpace(i.Name)
	if i.Name == "" || len([]rune(i.Name)) > 160 {
		return invalid("table name required, maximum 160 characters")
	}
	if err := i.Schema.Validate(); err != nil {
		return invalid(err.Error())
	}
	return nil
}
func GetTable(ownerID, id int64) (*table.V22DataTable, error) {
	row := new(table.V22DataTable)
	has, err := db.Instance().Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func ListTables(ownerID, before int64, limit int) ([]table.V22DataTable, string, error) {
	if limit < 1 || limit > 50 || before < 0 {
		return nil, "", invalid("limit/before out of range")
	}
	query := db.Instance().Where("owner_id=?", ownerID)
	if before > 0 {
		query.And("id<?", before)
	}
	rows := []table.V22DataTable{}
	err := query.Desc("id").Limit(limit + 1).Find(&rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.FormatInt(rows[len(rows)-1].Id, 10)
	}
	return rows, next, nil
}
func createTable(s *xorm.Session, ownerID int64, key string, input TableInput) (*table.V22DataTable, error) {
	if err := idempotency.Lock(s, ownerID, "data-table.create", key); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(input)
	fingerprint := capture.Hash(raw)
	previous := new(table.V22DataTable)
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
	if err := idempotency.Lock(s, ownerID, "data-table.capacity", "owner"); err != nil {
		return nil, err
	}
	count, err := s.Where("owner_id=?", ownerID).Count(new(table.V22DataTable))
	if err != nil {
		return nil, err
	}
	if count >= 100 {
		return nil, invalid("logical table limit (100) reached")
	}
	schemaRaw, _ := json.Marshal(input.Schema)
	row := &table.V22DataTable{OwnerId: ownerID, Name: input.Name, SchemaVersion: 1, Schema: string(schemaRaw), SchemaHash: output.SchemaHash(input.Schema), IdempotencyKey: key, Fingerprint: fingerprint, CreatedAt: time.Now()}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("INSERT INTO v22_data_table_schemas(table_id,version,schema,schema_hash) VALUES(?,1,?::jsonb,?)", row.Id, row.Schema, row.SchemaHash); err != nil {
		return nil, err
	}
	return row, nil
}
func CreateTable(ownerID int64, key string, input TableInput) (*table.V22DataTable, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	row, err := createTable(s, ownerID, key, input)
	if err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

type CheckInput struct {
	ExpectedRevision int              `json:"expected_revision"`
	CaptureID        string           `json:"capture_id,omitempty"`
	SampleID         string           `json:"sample_id,omitempty"`
	SampleRevision   int              `json:"sample_revision,omitempty"`
	StepID           string           `json:"step_id"`
	Stage            string           `json:"stage"`
	TableID          string           `json:"table_id,omitempty"`
	SchemaVersion    int              `json:"schema_version,omitempty"`
	Proposed         *TableInput      `json:"proposed_table,omitempty"`
	Mapping          []output.Mapping `json:"mapping"`
	UpdatePolicy     string           `json:"update_policy"`
	EmptyPolicy      string           `json:"empty_policy"`
}

func (i *CheckInput) Validate() error {
	if i.ExpectedRevision < 1 || (i.CaptureID == "") == (i.SampleID == "") || (i.TableID == "") == (i.Proposed == nil) || i.StepID == "" || len(i.StepID) > 128 || i.Stage != "list" && i.Stage != "detail" {
		return invalid("revision, input, step/role and existing/proposed table required")
	}
	if i.SampleID != "" {
		if _, err := uuid.Parse(i.SampleID); err != nil || i.SampleRevision < 1 {
			return invalid("sample ID/revision required")
		}
	} else {
		if _, err := uuid.Parse(i.CaptureID); err != nil {
			return invalid("invalid capture ID")
		}
		if i.SampleRevision != 0 {
			return invalid("sample_revision requires sample_id")
		}
	}
	if i.Proposed != nil {
		if i.SchemaVersion != 0 {
			return invalid("new table cannot specify existing schema version")
		}
		if err := i.Proposed.Validate(); err != nil {
			return err
		}
	} else {
		if id, err := strconv.ParseInt(i.TableID, 10, 64); err != nil || id < 1 || i.SchemaVersion < 1 {
			return invalid("table ID and schema version required")
		}
	}
	if err := output.ValidatePolicies(i.Mapping, i.UpdatePolicy, i.EmptyPolicy); err != nil {
		return invalid(err.Error())
	}
	return nil
}
func lockSample(s *xorm.Session, ownerID int64, id string, revision int) (*table.V22Sample, error) {
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
func lockCollector(s *xorm.Session, ownerID, id int64, revision int) (*table.V22Collector, error) {
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
func targetSchema(s *xorm.Session, ownerID int64, i CheckInput) (output.Schema, *table.V22DataTable, error) {
	if i.Proposed != nil {
		return i.Proposed.Schema, nil, nil
	}
	id, _ := strconv.ParseInt(i.TableID, 10, 64)
	if _, err := s.QueryString("SELECT id FROM v22_data_tables WHERE id=? AND owner_id=? FOR SHARE", id, ownerID); err != nil {
		return output.Schema{}, nil, err
	}
	row := new(table.V22DataTable)
	has, err := s.Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return output.Schema{}, nil, err
	}
	if !has {
		return output.Schema{}, nil, ErrNotFound
	}
	if row.SchemaVersion != i.SchemaVersion {
		return output.Schema{}, nil, ErrConflict
	}
	schema, err := output.DecodeSchema([]byte(row.Schema))
	if err != nil || output.SchemaHash(schema) != row.SchemaHash {
		return output.Schema{}, nil, invalid("table schema is corrupt")
	}
	return schema, row, nil
}
func source(s *xorm.Session, ownerID, collectorID int64, id string) (*table.V22Capture, error) {
	row := new(table.V22Capture)
	has, err := s.Where("id=? AND owner_id=? AND collector_id=?", id, ownerID, collectorID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if row.Status != "succeeded" || row.ContentHash != capture.Hash([]byte(row.Content)) {
		return nil, invalid("successful intact input required")
	}
	return row, nil
}
func existingRecords(s *xorm.Session, tableID int64, schema output.Schema, mapping []output.Mapping, result extraction.Result) (map[string]output.Existing, error) {
	existing := map[string]output.Existing{}
	if tableID == 0 {
		return existing, nil
	}
	seen := map[string]bool{}
	for _, record := range result.Records {
		prepared, issues := output.Prepare(schema, mapping, record)
		if len(issues) > 0 || seen[prepared.Key] {
			continue
		}
		seen[prepared.Key] = true
		rows, err := s.QueryString("SELECT id,revision,record_values::text AS record_values FROM v22_table_records WHERE table_id=? AND canonical_key=?", tableID, prepared.Key)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			values, err := output.DecodeValues(rows[0]["record_values"])
			if err != nil {
				return nil, err
			}
			revision, _ := strconv.Atoi(rows[0]["revision"])
			existing[prepared.Key] = output.Existing{RecordID: rows[0]["id"], Values: values, Revision: revision}
		}
	}
	return existing, nil
}
func CreateCheck(ctx context.Context, ownerID, collectorID int64, key string, input CheckInput) (*table.V22OutputCheck, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if err := validKey(key); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal([]any{collectorID, input})
	fingerprint := capture.Hash(raw)
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "output.check", key); err != nil {
		return nil, err
	}
	prior := new(table.V22OutputCheck)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return prior, nil
	}
	if input.SampleID != "" {
		sample, err := lockSample(s, ownerID, input.SampleID, input.SampleRevision)
		if err != nil {
			return nil, err
		}
		if sample.CollectorId != collectorID {
			return nil, ErrNotFound
		}
		if input.StepID != sample.StepId || input.Stage != sample.Stage {
			return nil, invalid("sample step/role cannot be replaced")
		}
		input.CaptureID = sample.CaptureId
	}
	collector, err := lockCollector(s, ownerID, collectorID, input.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	snapshot, err := source(s, ownerID, collectorID, input.CaptureID)
	if err != nil {
		return nil, err
	}
	plan, err := extraction.PlanFromDefinition([]byte(collector.Definition), input.StepID)
	if err != nil {
		return nil, invalid(err.Error())
	}
	result, err := extraction.Extract(ctx, extraction.Input{Content: snapshot.Content, Format: snapshot.Format, URL: snapshot.FinalURL, BaseURL: snapshot.BaseURL, Stage: input.Stage}, plan)
	if err != nil {
		return nil, invalid(err.Error())
	}
	schema, target, err := targetSchema(s, ownerID, input)
	if err != nil {
		return nil, err
	}
	compat := output.CheckCompatibility(schema, input.Mapping, output.SourceFields(plan, input.Stage))
	tableID := int64(0)
	version := 1
	if target != nil {
		tableID = target.Id
		version = target.SchemaVersion
	}
	existing, err := existingRecords(s, tableID, schema, input.Mapping, result)
	if err != nil {
		return nil, err
	}
	preview := output.PreviewBatch(schema, input.Mapping, input.UpdatePolicy, input.EmptyPolicy, result, compat, existing)
	config, _ := json.Marshal(input)
	payload, _ := json.Marshal(preview)
	if len(payload) > 2*capture.MaxBytes {
		return nil, invalid("output preview exceeds 2 MiB")
	}
	now := time.Now()
	row := &table.V22OutputCheck{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, CollectorRevision: collector.Revision, DefinitionHash: capture.Hash([]byte(collector.Definition)), CaptureId: snapshot.Id, ContentHash: snapshot.ContentHash, SampleId: input.SampleID, SampleRevision: input.SampleRevision, TableId: tableID, SchemaVersion: version, SchemaHash: output.SchemaHash(schema), Config: string(config), Result: string(payload), Ready: preview.Ready, IdempotencyKey: key, Fingerprint: fingerprint, ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now}
	insert := s
	if tableID == 0 {
		insert = s.Omit("table_id")
	}
	if _, err = insert.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("DELETE FROM v22_output_checks WHERE collector_id=? AND owner_id=? AND id NOT IN (SELECT check_id FROM v22_output_bindings WHERE collector_id=?) AND id NOT IN (SELECT output_check_id FROM v22_versions WHERE collector_id=?) AND id NOT IN (SELECT id FROM v22_output_checks WHERE collector_id=? AND owner_id=? ORDER BY created_at DESC,id DESC LIMIT 20)", collectorID, ownerID, collectorID, collectorID, collectorID, ownerID); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func GetCheck(ownerID, collectorID int64, id, key string) (*table.V22OutputCheck, error) {
	row := new(table.V22OutputCheck)
	query := db.Instance().Where("owner_id=? AND collector_id=?", ownerID, collectorID)
	if key != "" {
		query.And("idempotency_key=?", key)
	} else {
		query.And("id=?", id)
	}
	has, err := query.Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}

type ConfirmInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	CheckID          string `json:"check_id"`
}

func Confirm(ownerID, collectorID int64, input ConfirmInput) (*table.V22Collector, error) {
	if input.ExpectedRevision < 1 {
		return nil, invalid("revision required")
	}
	if _, err := uuid.Parse(input.CheckID); err != nil {
		return nil, invalid("invalid check_id")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	check := new(table.V22OutputCheck)
	has, err := s.Where("id=? AND owner_id=? AND collector_id=?", input.CheckID, ownerID, collectorID).Get(check)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	// Recover a committed confirmation before validating inputs that may have
	// changed since then. A single joined read keeps binding/revision consistent.
	if check.CollectorRevision == input.ExpectedRevision {
		replayed := new(table.V22Collector)
		has, err = s.SQL("SELECT c.* FROM v22_collectors c JOIN v22_output_bindings b ON b.collector_id=c.id AND b.owner_id=c.owner_id WHERE c.id=? AND c.owner_id=? AND c.status<>'archived' AND b.check_id=? AND b.bound_revision=c.revision", collectorID, ownerID, input.CheckID).Get(replayed)
		if err != nil {
			return nil, err
		}
		if has {
			return replayed, nil
		}
	}
	// Sample -> collector matches B02 check lock order.
	if check.SampleId != "" {
		if _, err = lockSample(s, ownerID, check.SampleId, check.SampleRevision); err != nil {
			return nil, err
		}
	}
	collector, err := lockCollector(s, ownerID, collectorID, input.ExpectedRevision)
	if err != nil {
		// A lost confirmation response may be replayed without incrementing revision.
		var conflict *v22_collector_repo.RevisionConflict
		if errors.As(err, &conflict) {
			binding := new(table.V22OutputBinding)
			has, lookupErr := s.Where("collector_id=? AND owner_id=? AND check_id=? AND bound_revision=?", collectorID, ownerID, input.CheckID, conflict.Latest.Revision).Get(binding)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if has && check.CollectorRevision == input.ExpectedRevision {
				return conflict.Latest, nil
			}
		}
		return nil, err
	}
	if !check.Ready {
		return nil, ErrBlocked
	}
	if !check.ExpiresAt.After(time.Now()) || check.CollectorRevision != collector.Revision || check.DefinitionHash != capture.Hash([]byte(collector.Definition)) {
		return nil, ErrConflict
	}
	snapshot, err := source(s, ownerID, collectorID, check.CaptureId)
	if err != nil {
		return nil, err
	}
	if snapshot.ContentHash != check.ContentHash {
		return nil, ErrConflict
	}
	config, err := decode[CheckInput](check.Config)
	if err != nil {
		return nil, err
	}
	schema, target, err := targetSchema(s, ownerID, config)
	if err != nil {
		return nil, err
	}
	if output.SchemaHash(schema) != check.SchemaHash {
		return nil, ErrConflict
	}
	if target == nil {
		target, err = createTable(s, ownerID, "output-"+check.Id, *config.Proposed)
		if err != nil {
			return nil, err
		}
	}
	bindingConfig := output.Config{TableID: strconv.FormatInt(target.Id, 10), SchemaVersion: target.SchemaVersion, SchemaHash: target.SchemaHash, StepID: config.StepID, Stage: config.Stage, Mapping: config.Mapping, UpdatePolicy: config.UpdatePolicy, EmptyPolicy: config.EmptyPolicy, CheckID: check.Id}
	bindingRaw, _ := json.Marshal(bindingConfig)
	definition, err := decode[map[string]json.RawMessage](collector.Definition)
	if err != nil {
		return nil, err
	}
	definition["output"] = bindingRaw
	nextDefinition, _ := json.Marshal(definition)
	collector.Definition = string(nextDefinition)
	collector.Revision++
	collector.UpdatedBy = ownerID
	collector.UpdatedAt = time.Now()
	if _, err = s.ID(collector.Id).Cols("definition", "revision", "updated_by", "updated_at").Update(collector); err != nil {
		return nil, err
	}
	if _, err = s.Exec("INSERT INTO v22_output_bindings(collector_id,owner_id,table_id,check_id,config,bound_revision) VALUES(?,?,?,?,?::jsonb,?) ON CONFLICT(collector_id) DO UPDATE SET table_id=EXCLUDED.table_id,check_id=EXCLUDED.check_id,config=EXCLUDED.config,bound_revision=EXCLUDED.bound_revision,created_at=NOW()", collectorID, ownerID, target.Id, check.Id, string(bindingRaw), collector.Revision); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return collector, nil
}
