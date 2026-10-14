package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"time"
	"xorm.io/xorm"
)

type UpgradeRequest struct {
	StageID            string `json:"stage_id"`
	CollectorVersionID string `json:"collector_version_id"`
}

func UpgradePreview(ctx context.Context, owner int64, taskID string, p UpgradeRequest) (any, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	t, e := getTask(s, owner, taskID, false)
	if e != nil {
		return nil, e
	}
	var d TaskDefinition
	if e = collector.Strict([]byte(t.Definition), &d); e != nil {
		return nil, e
	}
	var previous Version
	found := false
	for i, st := range d.Stages {
		if st.ID == p.StageID {
			previous, _, e = collectorVersion(s, owner, st.CollectorVersionID)
			if e != nil {
				return nil, e
			}
			d.Stages[i].CollectorVersionID = p.CollectorVersionID
			found = true
		}
	}
	if !found {
		return nil, collector.Invalid("stage_id", "阶段不存在")
	}
	next, _, e := collectorVersion(s, owner, p.CollectorVersionID)
	if e != nil {
		return nil, e
	}
	if previous.CollectorID != next.CollectorID {
		return nil, collector.Invalid("collector_version_id", "版本比较需要同一个采集器")
	}
	_, impact := buildManifest(s, owner, d)
	samples, e := views(s, "SELECT id,revision,input,expected_outputs,created_at FROM co_samples WHERE owner_id=? AND collector_id=? AND revision IN (?,?) ORDER BY created_at DESC LIMIT 20", owner, next.CollectorID, previous.Revision, next.Revision)
	if e != nil {
		return nil, e
	}
	var problem *collector.Error
	if impact != nil {
		problem = collector.AsError(impact)
	}
	return map[string]any{"expected_revision": t.Revision, "previous": previous.View(), "next": next.View(), "input_changed": previous.InputSchemaHash != next.InputSchemaHash, "output_changed": previous.OutputSchemaHash != next.OutputSchemaHash, "samples": samples, "compatible": impact == nil, "impact": problem, "proposed_definition": d, "active_runs": "unchanged; preview does not save or publish"}, nil
}
func ArchiveTask(ctx context.Context, owner int64, id, key string, revision int) (*Operation, error) {
	return Mutate(ctx, owner, "archiveTask", id, key, map[string]int{"expected_revision": revision}, false, func(s *xorm.Session, o *Operation) (any, error) {
		t, e := getTask(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if t.Revision != revision {
			return nil, conflict("任务已变化")
		}
		n, e := s.SQL(`SELECT COUNT(*) FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND r.status IN ('preparing','running','pausing','paused','awaiting_confirmation')`, id).Count(new(Run))
		if e != nil {
			return nil, e
		}
		if n > 0 {
			return nil, conflict("有活动运行，不能归档")
		}
		if _, e = s.Exec("UPDATE co_schedules SET enabled=FALSE,revision=revision+1 WHERE task_id=?", id); e != nil {
			return nil, e
		}
		// Archive pauses subscriptions and preserves their cursors/evidence.
		if _, e = s.Exec("UPDATE co_subscriptions SET enabled=FALSE,revision=revision+1 WHERE task_id=? AND status<>'retired'", id); e != nil {
			return nil, e
		}
		if _, e = s.Exec("UPDATE co_tasks SET archived=TRUE,updated_at=NOW() WHERE id=?", id); e != nil {
			return nil, e
		}
		return map[string]any{"id": id, "archived": true, "history": "retained"}, nil
	})
}

type SchemaUpgradeRequest struct {
	ExpectedVersion int         `json:"expected_version"`
	Schema          TableSchema `json:"schema"`
	Confirmed       bool        `json:"confirmed"`
}

// Schema changes do not migrate current records silently. Only additive optional
// fields preserve old bindings, identity and record types in this first version.
func schemaCompatibility(old, next TableSchema) error {
	if collector.Hash(old.UniqueKey) != collector.Hash(next.UniqueKey) {
		return collector.Invalid("schema.unique_key", "不能修改已有唯一键；请显式新建数据表")
	}
	fields := map[string]collector.Field{}
	for _, f := range next.Fields {
		fields[f.Key] = f
	}
	for _, f := range old.Fields {
		n, ok := fields[f.Key]
		if !ok || collector.Hash(f) != collector.Hash(n) {
			return collector.Invalid("schema."+f.Key, "现有字段不可删除或改变；请使用新表")
		}
		delete(fields, f.Key)
	}
	for _, f := range fields {
		if f.Required {
			return collector.Invalid("schema."+f.Key, "新增字段必须可缺省，不改写已有记录")
		}
	}
	return nil
}
func SchemaUpgrade(ctx context.Context, owner int64, id, key string, p SchemaUpgradeRequest) (*Operation, error) {
	if !p.Confirmed {
		return nil, collector.Invalid("confirmed", "Schema变更会要求任务重新预演绑定；请确认")
	}
	if e := p.Schema.Validate(); e != nil {
		return nil, e
	}
	return Mutate(ctx, owner, "upgradeSchema", id, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		t := new(DataTable)
		ok, e := s.SQL("SELECT * FROM co_data_tables WHERE owner_id=? AND id=? FOR UPDATE", owner, id).Get(t)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		if t.CurrentSchema != p.ExpectedVersion {
			return nil, conflict("Schema版本已变化")
		}
		// Do not acquire run locks after table locks; reject rather than race changes.
		n, e := s.SQL(`SELECT COUNT(*) FROM co_runs r WHERE r.owner_id=? AND r.status IN ('preparing','running','pausing','paused','awaiting_confirmation') AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.manifest -> 'definition' -> 'stages') st WHERE st -> 'save' ->> 'table_id'=? OR EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(st -> 'additional_saves','[]')) p WHERE p -> 'save' ->> 'table_id'=?))`, owner, id, id).Count(new(Run))
		if e != nil {
			return nil, e
		}
		if n > 0 {
			return nil, conflict("此表有活动运行，请结束后升级Schema")
		}
		sv := new(SchemaVersion)
		if _, e = s.Where("table_id=? AND version=?", id, t.CurrentSchema).Get(sv); e != nil {
			return nil, e
		}
		var old TableSchema
		if e = collector.Strict([]byte(sv.Schema), &old); e != nil {
			return nil, e
		}
		if e = schemaCompatibility(old, p.Schema); e != nil {
			return nil, e
		}
		next := SchemaVersion{TableID: id, Version: t.CurrentSchema + 1, Schema: collector.JSON(p.Schema), SchemaHash: collector.Hash(p.Schema)}
		if _, e = s.Insert(&next); e != nil {
			return nil, e
		}
		if _, e = s.Exec("UPDATE co_data_tables SET current_schema_version=? WHERE id=?", next.Version, id); e != nil {
			return nil, e
		}
		return map[string]any{"id": id, "schema_version": next.Version, "schema_hash": next.SchemaHash, "schema": p.Schema, "records": "old revisions retained; re-preview task bindings required"}, nil
	})
}

type ExportRequest struct {
	RecordIDs []string `json:"record_ids"`
	Limit     int      `json:"limit"`
}

func ExportTable(ctx context.Context, owner int64, id string, p ExportRequest) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if p.Limit < 1 || p.Limit > 1000 || len(p.RecordIDs) > 1000 {
		return nil, collector.Invalid("limit", "导出需要1..1000条明确边界")
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	if _, e := s.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); e != nil {
		return nil, e
	}
	if _, e := s.Exec("SET LOCAL statement_timeout='8000ms'"); e != nil {
		return nil, e
	}
	table, e := views(s, `SELECT t.id,t.name,s.version,s.schema_hash,s.schema FROM co_data_tables t JOIN co_schema_versions s ON s.table_id=t.id AND s.version=t.current_schema_version WHERE t.owner_id=? AND t.id=?`, owner, id)
	if e != nil {
		return nil, e
	}
	if len(table) == 0 {
		return nil, ErrNotFound
	}
	q := "SELECT id,values_json AS values,revision,schema_version,last_seen_at FROM co_records WHERE table_id=?"
	args := []any{id}
	if len(p.RecordIDs) > 0 {
		q += " AND id IN (SELECT jsonb_array_elements_text(CAST(? AS jsonb)))"
		args = append(args, collector.JSON(p.RecordIDs))
	}
	q += " ORDER BY id LIMIT ?"
	args = append(args, p.Limit+1)
	rows, e := views(s, q, args...)
	if e != nil {
		return nil, e
	}
	if len(rows) > p.Limit {
		return nil, collector.Invalid("limit", "导出超过边界；请勾选记录或提高上限")
	}
	if len(p.RecordIDs) > 0 && len(rows) != len(p.RecordIDs) {
		return nil, collector.Invalid("record_ids", "所选记录缺失或重复")
	}
	if len(collector.JSON(rows)) > 4*1024*1024 {
		return nil, collector.Invalid("limit", "导出超过4MiB，请缩小批次")
	}
	return map[string]any{"table": json.RawMessage(table[0]), "records": rows, "count": len(rows), "limit": p.Limit, "ordering": "record_id ascending", "content_hash": collector.Hash(rows), "boundary": "repeatable-read snapshot; current records with explicit revisions; historical run evidence is queried separately"}, nil
}
