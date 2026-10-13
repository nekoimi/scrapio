package co

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"time"
)

func Runs(ctx context.Context, owner int64, cursor string, limit int) (any, error) {
	return domainList(ctx, owner, "co_runs", cursor, limit)
}
func RunDetail(ctx context.Context, owner int64, id string, summary ...bool) (any, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getRun(s, owner, id, false); e != nil {
		return nil, e
	}
	projection := "*"
	if len(summary) > 0 && summary[0] {
		projection = "id,task_version_id,namespace,status,control_epoch,preparing_cursor,input_count,output_count,event_seq,retry_of,retry_scope,created_at,finished_at"
	}
	rows, e := views(s, "SELECT "+projection+" FROM co_runs WHERE id=?", id)
	if e != nil {
		return nil, e
	}
	var runView map[string]any
	_ = json.Unmarshal(rows[0], &runView)
	taskRows, e := s.QueryString("SELECT task_id FROM co_task_versions WHERE id=?", runView["task_version_id"])
	if e != nil {
		return nil, e
	}
	if len(taskRows) > 0 {
		runView["task_id"] = taskRows[0]["task_id"]
	}
	rows[0] = json.RawMessage(collector.JSON(runView))
	stages, e := views(s, `SELECT st.*,
 (SELECT COUNT(*) FROM co_work_items w WHERE w.stage_run_id=st.id) AS input_count,
 (SELECT COUNT(*) FROM co_item_sources src JOIN co_work_items w ON w.id=src.item_id WHERE w.stage_run_id=st.id) AS source_count,
 (SELECT COUNT(*) FROM co_item_sources src JOIN co_work_items w ON w.id=src.item_id WHERE w.stage_run_id=st.id AND src.merged) AS merged_inputs,
 (SELECT COUNT(*) FROM co_run_outputs o JOIN co_work_items w ON w.id=o.item_id WHERE w.stage_run_id=st.id) AS output_count,
 (SELECT COALESCE(SUM(p.total-p.next_ordinal),0) FROM co_dispatch_plans p WHERE p.run_id=st.run_id AND p.from_stage=st.stage_id AND p.status='pending') AS pending_dispatches,
 (SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE p.run_id=st.run_id AND p.from_stage=st.stage_id AND d.status='delivered') AS delivered,
 (SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE p.run_id=st.run_id AND p.from_stage=st.stage_id AND d.status='merged') AS merged_dispatches,
 (SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE p.run_id=st.run_id AND p.from_stage=st.stage_id AND d.status='filtered') AS filtered,
 (SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE p.run_id=st.run_id AND p.from_stage=st.stage_id AND d.status IN ('invalid','limited')) AS dispatch_errors,
 (SELECT COALESCE(jsonb_object_agg(decision,n),'{}') FROM (SELECT o.decision,COUNT(*) AS n FROM co_observations o JOIN co_work_items w ON w.id=o.item_id WHERE w.stage_run_id=st.id GROUP BY o.decision) decisions) AS save_decisions
 FROM co_stage_runs st WHERE st.run_id=? ORDER BY stage_id`, id)
	if e != nil {
		return nil, e
	}
	counts, e := views(s, "SELECT status,COUNT(*) AS count FROM co_work_items WHERE run_id=? GROUP BY status", id)
	return map[string]any{"run": rows[0], "stages": stages, "counts": counts}, e
}
func RunPage(ctx context.Context, owner int64, id, kind, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getRun(s, owner, id, false); e != nil {
		return nil, e
	}
	table := ""
	switch kind {
	case "work-items":
		table = "co_work_items"
	case "outputs":
		table = "co_run_outputs"
	case "observations":
		table = "co_observations"
	case "dispatches":
		table = "co_dispatches"
	default:
		return nil, ErrNotFound
	}
	projection := "*"
	if kind == "outputs" || kind == "observations" {
		projection = "*,values_json AS values"
	}
	q := "SELECT " + projection + " FROM " + table + " WHERE run_id=?"
	args := []any{id}
	if cursor != "" {
		b, e := decodeCursor(cursor)
		if e != nil {
			return nil, e
		}
		q += " AND (created_at,id)<(?,?)"
		args = append(args, b.At, b.ID)
	}
	q += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, e := views(s, q, args...)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		var last struct {
			ID        string
			CreatedAt time.Time `json:"created_at"`
		}
		_ = json.Unmarshal(rows[limit-1], &last)
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return map[string]any{"items": rows, "next_cursor": next}, nil
}
func ItemDetail(ctx context.Context, owner int64, runID, itemID string, sourceCursors ...string) (any, error) {
	cursor := ""
	if len(sourceCursors) > 0 {
		cursor = sourceCursors[0]
	}
	if cursor != "" {
		if _, e := uuid.Parse(cursor); e != nil {
			return nil, collector.Invalid("source_cursor", "来源游标无效")
		}
	}
	s := session(ctx)
	defer s.Close()
	if _, e := getRun(s, owner, runID, false); e != nil {
		return nil, e
	}
	rows, e := views(s, "SELECT * FROM co_work_items WHERE run_id=? AND id=?", runID, itemID)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	attempts, e := views(s, "SELECT * FROM co_work_attempts WHERE run_id=? AND item_id=? ORDER BY number DESC LIMIT 100", runID, itemID)
	if e != nil {
		return nil, e
	}
	input, e := views(s, "SELECT i.*,i.values_json AS values FROM co_run_inputs i JOIN co_work_items w ON w.input_id=i.id WHERE w.run_id=? AND w.id=?", runID, itemID)
	if e != nil {
		return nil, e
	}
	sources, e := views(s, `SELECT i.*,i.values_json AS values,src.merged FROM co_item_sources src JOIN co_run_inputs i ON i.id=src.input_id WHERE src.run_id=? AND src.item_id=? AND i.id>? ORDER BY i.id LIMIT 101`, runID, itemID, cursor)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(sources) > 100 {
		sources = sources[:100]
		var last struct{ ID string }
		_ = json.Unmarshal(sources[99], &last)
		next = last.ID
	}
	lineage, e := views(s, `WITH RECURSIVE chain AS (
 SELECT l.*,1 AS depth FROM co_lineage l WHERE l.run_id=? AND l.target_item_id=?
 UNION SELECT l.*,c.depth+1 FROM co_lineage l JOIN chain c ON l.target_item_id=c.source_item_id AND l.run_id=c.run_id WHERE c.depth<20
 ) SELECT c.*,o.values_json AS output_values,o.output_identity,o.source,w.input_values AS source_input,st.stage_id AS source_stage
 FROM chain c JOIN co_run_outputs o ON o.id=c.source_output_id JOIN co_work_items w ON w.id=c.source_item_id JOIN co_stage_runs st ON st.id=w.stage_run_id ORDER BY depth,c.source_output_id,c.edge_id,c.element_key LIMIT 101`, runID, itemID)
	more := len(lineage) > 100
	if more {
		lineage = lineage[:100]
	}
	return map[string]any{"item": rows[0], "attempts": attempts, "input": input, "sources": sources, "next_source_cursor": next, "lineage": lineage, "lineage_has_more": more}, e
}
func RunEvents(ctx context.Context, owner int64, id string, after int64) (any, error) {
	if after < 0 {
		return nil, collector.Invalid("after", "事件序号不可为负")
	}
	s := session(ctx)
	defer s.Close()
	r, e := getRun(s, owner, id, false)
	if e != nil {
		return nil, e
	}
	rows, e := views(s, "SELECT * FROM co_run_events WHERE run_id=? AND sequence>? ORDER BY sequence LIMIT 100", id, after)
	if e != nil {
		return nil, e
	}
	next := after
	for _, raw := range rows {
		var v struct{ Sequence int64 }
		_ = json.Unmarshal(raw, &v)
		next = v.Sequence
	}
	return map[string]any{"items": rows, "next_sequence": next, "latest_sequence": r.EventSeq, "status": r.Status}, nil
}
func OriginalCommand(ctx context.Context, owner int64, runID, itemID, attemptID, commandID string) (*Attempt, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getRun(s, owner, runID, false); e != nil {
		return nil, e
	}
	a := new(Attempt)
	ok, e := s.Where("run_id=? AND item_id=? AND id=?", runID, itemID, attemptID).Get(a)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	var refs []struct {
		CommandID string `json:"command_id"`
	}
	if e = json.Unmarshal([]byte(a.CommandRefs), &refs); e != nil {
		return nil, e
	}
	for _, ref := range refs {
		if ref.CommandID == commandID {
			return a, nil
		}
	}
	return nil, ErrNotFound
}
func TableRecords(ctx context.Context, owner int64, tableID, cursor string, limit int) (any, error) {
	s := session(ctx)
	defer s.Close()
	t := new(DataTable)
	ok, e := s.Where("owner_id=? AND id=?", owner, tableID).Get(t)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, e := views(s, `SELECT id,values_json AS values,revision,schema_version,last_seen_at FROM co_records WHERE table_id=? AND id>? ORDER BY id LIMIT ?`, tableID, cursor, limit+1)
	if e != nil {
		return nil, e
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		var last struct{ ID string }
		_ = json.Unmarshal(rows[limit-1], &last)
		next = last.ID
	}
	return map[string]any{"items": rows, "next_cursor": next}, nil
}
func RecordHistory(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	rows, e := views(s, `SELECT r.id,r.values_json AS values,r.revision,r.schema_version,r.last_seen_at,t.id AS table_id FROM co_records r JOIN co_data_tables t ON t.id=r.table_id WHERE t.owner_id=? AND r.id=?`, owner, id)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	observations, e := views(s, `SELECT o.id,o.run_id,o.item_id,o.output_identity,o.values_json AS values,o.decision,o.created_at,w.input_id,w.source_key FROM co_observations o JOIN co_work_items w ON w.id=o.item_id JOIN co_runs r ON r.id=o.run_id WHERE o.record_id=? AND r.owner_id=? ORDER BY o.created_at DESC,o.id DESC LIMIT 100`, id, owner)
	if e != nil {
		return nil, e
	}
	revisions, e := views(s, `SELECT revision,observation_id,values_json AS values,created_at FROM co_record_revisions WHERE record_id=? ORDER BY revision DESC LIMIT 100`, id)
	return map[string]any{"record": rows[0], "observations": observations, "revisions": revisions}, e
}
