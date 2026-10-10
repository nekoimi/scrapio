package v22_governance_repo

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/governance"
	"github.com/nekoimi/scrapio/internal/output"
	"xorm.io/xorm"
)

type SchemaInput struct {
	ExpectedVersion int           `json:"expected_schema_version"`
	Schema          output.Schema `json:"schema"`
}
type SchemaImpact struct {
	CheckID         string        `json:"check_id"`
	TableID         string        `json:"table_id"`
	ExpectedVersion int           `json:"expected_schema_version"`
	AppliedVersion  int           `json:"applied_version"`
	Schema          output.Schema `json:"schema"`
	governance.SchemaChanges
	RecordCount          int64               `json:"record_count"`
	RecordVersions       []map[string]string `json:"record_versions"`
	Dependencies         []map[string]string `json:"dependencies"`
	SampledRecords       int                 `json:"sampled_records"`
	HistoricalViolations []map[string]any    `json:"historical_violations"`
	AnalysisTruncated    bool                `json:"analysis_truncated"`
	ExpiresAt            time.Time           `json:"expires_at"`
}

func lockTable(s *xorm.Session, owner, id int64) (*table.V22DataTable, error) {
	if _, err := s.QueryString("SELECT id FROM v22_data_tables WHERE owner_id=? AND id=? FOR UPDATE", owner, id); err != nil {
		return nil, err
	}
	row := new(table.V22DataTable)
	has, err := s.Where("owner_id=? AND id=?", owner, id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}

// Include mutable bindings, schedules, views, versions and live runs. Existing
// output/run writers serialize on the same logical table before using its schema.
func dependencies(s *xorm.Session, owner, id int64) ([]map[string]string, []map[string]string, error) {
	rows, err := s.QueryString(`SELECT * FROM (
 SELECT 'draft' AS kind,c.id::text AS id,c.name,c.revision::text AS revision,c.status AS state FROM v22_collectors c
 WHERE c.owner_id=? AND c.definition->'output'->>'table_id'=?
 UNION ALL SELECT 'version',v.id,v.name,v.number::text,'immutable' FROM v22_versions v WHERE v.owner_id=? AND v.definition->'output'->>'table_id'=?
 UNION ALL SELECT 'run',r.id,''::text,r.attempt::text,r.status FROM v22_runs r WHERE r.owner_id=? AND r.definition->'output'->>'table_id'=? AND r.status IN ('queued','running')
 UNION ALL SELECT 'schedule',s.collector_id::text,''::text,s.revision::text,CASE WHEN s.enabled THEN 'enabled' ELSE 'disabled' END FROM v22_schedules s JOIN v22_versions v ON v.id=(s.input->>'version_id') WHERE s.owner_id=? AND v.definition->'output'->>'table_id'=?
 UNION ALL SELECT 'api_trigger',k.id,k.name,'1','active' FROM v22_api_keys k JOIN v22_versions v ON v.id=(k.input->>'version_id') WHERE k.owner_id=? AND k.revoked_at IS NULL AND v.definition->'output'->>'table_id'=?
 UNION ALL SELECT 'view',v.id,v.name,v.revision::text,'saved' FROM v22_data_views v WHERE v.owner_id=? AND v.table_id=?
 ) refs ORDER BY kind,id LIMIT 201`, owner, strconv.FormatInt(id, 10), owner, strconv.FormatInt(id, 10), owner, strconv.FormatInt(id, 10), owner, strconv.FormatInt(id, 10), owner, strconv.FormatInt(id, 10), owner, id)
	if err != nil {
		return nil, nil, err
	}
	if rows == nil {
		rows = []map[string]string{}
	}
	versions, err := s.QueryString("SELECT schema_version::text,count(*)::text AS count,COALESCE(sum(revision),0)::text AS revision_sum FROM v22_table_records WHERE table_id=? GROUP BY schema_version ORDER BY schema_version", id)
	if versions == nil {
		versions = []map[string]string{}
	}
	return rows, versions, err
}
func dependencyHash(row *table.V22DataTable, refs, versions []map[string]string) string {
	raw, _ := json.Marshal([]any{row.SchemaVersion, row.SchemaHash, refs, versions})
	return capture.Hash(raw)
}
func CreateSchemaCheck(ctx context.Context, owner, id int64, input SchemaInput) (*SchemaImpact, error) {
	if input.ExpectedVersion < 1 || input.Schema.Validate() != nil {
		return nil, ErrInvalid
	}
	s, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	defer s.Rollback()
	row, err := lockTable(s, owner, id)
	if err != nil {
		return nil, err
	}
	if row.SchemaVersion != input.ExpectedVersion {
		return nil, ErrConflict
	}
	old, err := output.DecodeSchema([]byte(row.Schema))
	if err != nil {
		return nil, err
	}
	refs, versions, err := dependencies(s, owner, id)
	if err != nil {
		return nil, err
	}
	result := &SchemaImpact{CheckID: uuid.NewString(), TableID: strconv.FormatInt(id, 10), ExpectedVersion: row.SchemaVersion, Schema: input.Schema, SchemaChanges: governance.CompareSchema(old, input.Schema), RecordVersions: versions, Dependencies: refs, HistoricalViolations: []map[string]any{}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	for _, ref := range refs {
		if ref["kind"] == "run" {
			result.Blockers = append(result.Blockers, "ACTIVE_RUNS_MUST_FINISH")
		}
		if ref["kind"] == "schedule" && ref["state"] == "enabled" {
			result.Blockers = append(result.Blockers, "PAUSE_SCHEDULE_BEFORE_SCHEMA_CHANGE")
		}
	}
	if row.SchemaVersion >= 100 {
		result.Blockers = append(result.Blockers, "SCHEMA_VERSION_LIMIT_REQUIRES_NEW_TABLE")
	}
	if len(refs) > 200 {
		result.Blockers = append(result.Blockers, "DEPENDENCY_PREVIEW_LIMIT_EXCEEDED")
	}
	for _, v := range versions {
		count, _ := strconv.ParseInt(v["count"], 10, 64)
		result.RecordCount += count
	}
	records, err := s.QueryString("SELECT id,schema_version::text,record_values::text FROM v22_table_records WHERE table_id=? ORDER BY id LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	result.SampledRecords = len(records)
	result.AnalysisTruncated = result.RecordCount > int64(len(records))
	for _, r := range records {
		values, err := output.DecodeValues(r["record_values"])
		if err != nil {
			return nil, err
		}
		issues := governance.HistoricalIssues(input.Schema, values)
		if len(issues) > 0 {
			result.HistoricalViolations = append(result.HistoricalViolations, map[string]any{"record_id": r["id"], "schema_version": r["schema_version"], "issues": issues})
		}
	}
	raw, _ := json.Marshal(result)
	schema, _ := json.Marshal(input.Schema)
	if _, err = s.Exec("INSERT INTO v22_schema_checks(id,owner_id,table_id,expected_version,schema,impact,dependency_hash,expires_at) VALUES(?,?,?,?,?::jsonb,?::jsonb,?,?)", result.CheckID, owner, id, input.ExpectedVersion, string(schema), string(raw), dependencyHash(row, refs, versions), result.ExpiresAt); err != nil {
		return nil, err
	}
	if _, err = s.Exec("DELETE FROM v22_schema_checks WHERE id IN (SELECT id FROM v22_schema_checks WHERE owner_id=? AND table_id=? AND applied_version=0 ORDER BY created_at DESC,id DESC OFFSET 20)", owner, id); err != nil {
		return nil, err
	}
	return result, s.Commit()
}
func ApplySchema(ctx context.Context, owner, id int64, checkID string, confirmed bool) (*SchemaImpact, error) {
	if !confirmed {
		return nil, ErrInvalid
	}
	if _, err := uuid.Parse(checkID); err != nil {
		return nil, ErrInvalid
	}
	s, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	defer s.Rollback()
	row, err := lockTable(s, owner, id)
	if err != nil {
		return nil, err
	}
	checks, err := s.QueryString("SELECT impact::text,dependency_hash,applied_version::text,expires_at>clock_timestamp() AS live FROM v22_schema_checks WHERE id=? AND owner_id=? AND table_id=? FOR UPDATE", checkID, owner, id)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, ErrNotFound
	}
	var impact SchemaImpact
	if err = json.Unmarshal([]byte(checks[0]["impact"]), &impact); err != nil {
		return nil, err
	}
	impact.AppliedVersion = int(number(checks, "applied_version"))
	if impact.AppliedVersion > 0 {
		return &impact, nil
	}
	if checks[0]["live"] != "true" || len(impact.Blockers) > 0 {
		return nil, ErrConflict
	}
	refs, versions, err := dependencies(s, owner, id)
	if err != nil {
		return nil, err
	}
	if row.SchemaVersion != impact.ExpectedVersion || dependencyHash(row, refs, versions) != checks[0]["dependency_hash"] {
		return nil, ErrConflict
	}
	raw, _ := json.Marshal(impact.Schema)
	hash := output.SchemaHash(impact.Schema)
	impact.AppliedVersion = row.SchemaVersion + 1
	if _, err = s.Exec("INSERT INTO v22_data_table_schemas(table_id,version,schema,schema_hash) VALUES(?,?,?::jsonb,?)", id, impact.AppliedVersion, string(raw), hash); err != nil {
		return nil, err
	}
	if _, err = s.Exec("UPDATE v22_data_tables SET schema_version=?,schema=?::jsonb,schema_hash=? WHERE id=? AND owner_id=?", impact.AppliedVersion, string(raw), hash, id, owner); err != nil {
		return nil, err
	}
	if _, err = s.Exec("UPDATE v22_schema_checks SET applied_version=? WHERE id=?", impact.AppliedVersion, checkID); err != nil {
		return nil, err
	}
	if err = Record(s, owner, "schema.publish", strconv.FormatInt(id, 10), governance.SafeDetails{FromVersion: row.SchemaVersion, ToVersion: impact.AppliedVersion}); err != nil {
		return nil, err
	}
	return &impact, s.Commit()
}
func SchemaHistory(ctx context.Context, owner, id int64) ([]map[string]string, error) {
	rows, err := db.Instance().Context(ctx).QueryString("SELECT s.version::text,s.schema::text,s.schema_hash,s.created_at::text FROM v22_data_table_schemas s JOIN v22_data_tables t ON t.id=s.table_id WHERE t.owner_id=? AND t.id=? ORDER BY s.version DESC LIMIT 100", owner, id)
	if err == nil && len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows, err
}
