// Package v22_overview_repo reads only the owner's isolated v2.2 product domain.
package v22_overview_repo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/overview"
	"xorm.io/xorm"
)

var ErrNotFound = errors.New("collector not found")

const sectionLimit = 6

// SQL constructs a deliberately small allowlist; definitions, input, record
// values, lease tokens, credentials and browser sessions never leave this reader.
const runBrief = `jsonb_build_object('run_id',r.id,'collector_id',r.collector_id::text,'version_number',r.version_number,
 'status',r.status,'stop_reason',COALESCE(r.summary->>'stop_reason',''),
 'committed',COALESCE((r.summary->>'committed')::boolean,false),'counts',COALESCE(r.summary->'counts','{}'::jsonb),
 'table_id',COALESCE(t.id::text,''),'created_at',r.created_at,'finished_at',r.finished_at)`

// Latest completed and latest effective runs are independent from latest queued
// or running work, so another trigger cannot erase the last completed diagnosis.
const healthSQL = `WITH runs AS (
 SELECT r.id,r.collector_id,r.created_at,r.finished_at,r.status,COALESCE((r.summary->>'committed')::boolean,false) AS committed,` + runBrief + ` AS brief FROM v22_runs r
 LEFT JOIN v22_data_tables t ON t.id::text=r.definition->'output'->>'table_id' AND t.owner_id=r.owner_id
 WHERE r.owner_id=?
), latest AS (SELECT DISTINCT ON(collector_id) collector_id,brief,created_at FROM runs ORDER BY collector_id,created_at DESC,id DESC),
 terminal AS (SELECT DISTINCT ON(collector_id) collector_id,brief,status FROM runs WHERE status NOT IN ('queued','running') ORDER BY collector_id,finished_at DESC NULLS LAST,id DESC),
 effective AS (SELECT DISTINCT ON(collector_id) collector_id,brief FROM runs WHERE status IN ('succeeded','limited') AND committed ORDER BY collector_id,finished_at DESC NULLS LAST,id DESC),
 active AS (SELECT collector_id,count(*) AS count FROM runs WHERE status IN ('queued','running') GROUP BY collector_id)
 SELECT jsonb_build_object('as_of',CURRENT_TIMESTAMP,'collector_id',c.id::text,'name',c.name,'entry_type',c.entry_type,'archived',c.status='archived',
 'published_version_id',v.id,'pending_draft',v.id IS NULL OR c.revision>v.collector_revision,
 'table_id',COALESCE(t.id::text,''),'table_name',COALESCE(t.name,''),'updated_at',c.updated_at,
 'active_runs',COALESCE(a.count,0),'latest_run',l.brief,'latest_terminal',f.brief,'latest_effective',e.brief,
 'baseline_status',CASE WHEN EXISTS(SELECT 1 FROM v22_quality_policies qp WHERE qp.collector_id=c.id AND qp.owner_id=c.owner_id AND qp.baseline<>'{}') THEN 'selected' ELSE 'not_configured' END,
 'quality_issues',COALESCE((SELECT jsonb_agg(q.brief) FROM (SELECT jsonb_build_object('issue_id',qi.id,'status',qi.status,'kind','quality_issue','severity',CASE WHEN qi.status='ready' THEN 'warning' ELSE 'error' END,'code',qi.code,'run_id',qi.last_run_id) AS brief FROM v22_quality_issues qi WHERE qi.collector_id=c.id AND qi.owner_id=c.owner_id AND qi.status<>'resolved' ORDER BY qi.updated_at DESC,qi.id DESC LIMIT 6) q),'[]'),
 'schedule',CASE WHEN s.collector_id IS NULL THEN NULL ELSE jsonb_build_object('enabled',s.enabled,'next_at',s.next_at,'timezone',s.timezone,
 'version_id',COALESCE(sv.id,''),'version_number',COALESCE(sv.number,0),'last_decision',s.last_decision,
 'decision_at',(SELECT se.created_at FROM v22_schedule_events se WHERE se.collector_id=c.id ORDER BY se.id DESC LIMIT 1)) END)::text AS value
 FROM v22_collectors c
 LEFT JOIN v22_versions v ON v.id=c.published_version_id AND v.owner_id=c.owner_id AND v.collector_id=c.id
 LEFT JOIN v22_data_tables t ON t.id::text=v.definition->'output'->>'table_id' AND t.owner_id=c.owner_id
 LEFT JOIN latest l ON l.collector_id=c.id LEFT JOIN terminal f ON f.collector_id=c.id
 LEFT JOIN effective e ON e.collector_id=c.id LEFT JOIN active a ON a.collector_id=c.id
 LEFT JOIN v22_schedules s ON s.collector_id=c.id AND s.owner_id=c.owner_id
 LEFT JOIN v22_versions sv ON sv.id=s.input->>'version_id' AND sv.owner_id=c.owner_id AND sv.collector_id=c.id
 WHERE c.owner_id=?`

func query(s *xorm.Session, sql string, args ...any) ([]map[string]string, error) {
	return s.QueryString(append([]any{sql}, args...)...)
}
func healthRows(s *xorm.Session, owner int64, clause string, args ...any) ([]overview.Health, error) {
	params := append([]any{owner, owner}, args...)
	rows, err := query(s, healthSQL+clause, params...)
	if err != nil {
		return nil, err
	}
	items := []overview.Health{}
	for _, row := range rows {
		var h overview.Health
		if err = json.Unmarshal([]byte(row["value"]), &h); err != nil {
			return nil, err
		}
		overview.Evaluate(&h)
		items = append(items, h)
	}
	return items, nil
}

func Health(ctx context.Context, owner, id int64) (*overview.Health, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	rows, err := healthRows(s, owner, " AND c.id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

const attentionClause = ` AND c.status<>'archived' AND (f.status IN ('failed','partial','limited') OR s.last_decision IN ('disabled_invalid','disabled_unavailable','skipped_capacity') OR EXISTS(SELECT 1 FROM v22_quality_issues qi WHERE qi.owner_id=c.owner_id AND qi.collector_id=c.id AND qi.status<>'resolved'))`

type Page struct {
	Items []overview.Health `json:"items"`
	Next  string            `json:"next_cursor"`
	More  bool              `json:"has_more"`
}

func List(ctx context.Context, owner int64, attention bool, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 50 {
		return Page{}, errors.New("limit must be 1..50")
	}
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	clause := " AND c.status<>'archived'"
	if attention {
		clause = attentionClause
	}
	params := []any{}
	if cursor != "" {
		id, err := PositiveID(cursor)
		if err != nil {
			return Page{}, err
		}
		anchor, err := healthRows(s, owner, " AND c.id=?", id)
		if err != nil {
			return Page{}, err
		}
		if len(anchor) == 0 || anchor[0].Archived || attention && len(anchor[0].Issues) == 0 {
			return Page{}, ErrNotFound
		}
		clause += " AND c.id<?"
		params = append(params, id)
	}
	params = append(params, limit+1)
	items, err := healthRows(s, owner, clause+" ORDER BY c.id DESC LIMIT ?", params...)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items}
	if len(items) > limit {
		p.More = true
		p.Items = items[:limit]
		p.Next = p.Items[limit-1].CollectorID
	}
	return p, nil
}

func Home(ctx context.Context, owner int64) (*overview.Home, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if _, err := s.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		return nil, err
	}
	h := &overview.Home{Status: "ready", Baseline: "per_collector", Limit: sectionLimit}
	rows, err := query(s, `SELECT json_build_object('as_of',CURRENT_TIMESTAMP,'window_start',CURRENT_TIMESTAMP-interval '24 hours')::text AS value`)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(rows[0]["value"]), h); err != nil {
		return nil, err
	}
	rows, err = query(s, `SELECT
 (SELECT count(*)::text FROM v22_collectors WHERE owner_id=? AND status<>'archived') AS collectors,
 (SELECT count(*)::text FROM v22_collectors c LEFT JOIN v22_versions v ON v.id=c.published_version_id AND v.owner_id=c.owner_id AND v.collector_id=c.id WHERE c.owner_id=? AND c.status<>'archived' AND (v.id IS NULL OR c.revision>v.collector_revision)) AS pending_drafts,
 (SELECT count(*)::text FROM v22_data_tables WHERE owner_id=?) AS tables,
 (SELECT count(*)::text FROM v22_table_records r JOIN v22_data_tables t ON t.id=r.table_id WHERE t.owner_id=?) AS records,
 (SELECT count(*)::text FROM v22_runs WHERE owner_id=? AND status IN ('queued','running')) AS active_runs`, owner, owner, owner, owner, owner)
	if err != nil {
		return nil, err
	}
	h.Counts = rows[0]
	rows, err = query(s, `SELECT COALESCE(sum((summary->'counts'->>'created')::bigint),0)::text AS created,
 COALESCE(sum((summary->'counts'->>'updated')::bigint),0)::text AS updated,
 COALESCE(sum((summary->'counts'->>'unchanged')::bigint),0)::text AS unchanged,
 count(*)::text AS effective_runs FROM v22_runs WHERE owner_id=? AND summary->>'committed'='true'
 AND status IN ('succeeded','limited') AND finished_at>=CURRENT_TIMESTAMP-interval '24 hours' AND finished_at<=CURRENT_TIMESTAMP`, owner)
	if err != nil {
		return nil, err
	}
	h.Activity = rows[0]
	// Only one small result page per section; total attention is counted separately.
	countRows, err := query(s, `SELECT count(*)::text AS count FROM (`+healthSQL+attentionClause+`) h`, owner, owner)
	if err != nil {
		return nil, err
	}
	h.Counts["attention"] = countRows[0]["count"]
	if h.Attention, err = healthRows(s, owner, attentionClause+" ORDER BY COALESCE((f.brief->>'finished_at')::timestamptz,c.updated_at) DESC,c.id DESC LIMIT ?", sectionLimit); err != nil {
		return nil, err
	}
	if h.RecentCollectors, err = healthRows(s, owner, " AND c.status<>'archived' ORDER BY GREATEST(c.updated_at,l.created_at) DESC,c.id DESC LIMIT ?", sectionLimit); err != nil {
		return nil, err
	}
	if h.PendingDrafts, err = healthRows(s, owner, " AND c.status<>'archived' AND (v.id IS NULL OR c.revision>v.collector_revision) ORDER BY c.updated_at DESC,c.id DESC LIMIT ?", sectionLimit); err != nil {
		return nil, err
	}
	if h.NextSchedules, err = healthRows(s, owner, " AND c.status<>'archived' AND s.enabled AND s.next_at IS NOT NULL ORDER BY s.next_at,c.id LIMIT ?", sectionLimit); err != nil {
		return nil, err
	}
	rows, err = query(s, `SELECT jsonb_build_object('table_id',t.id::text,'name',t.name,'record_count',
 (SELECT count(*)::text FROM v22_table_records r WHERE r.table_id=t.id),
 'last_observed_at',(SELECT max(r.last_observed_at) FROM v22_table_records r WHERE r.table_id=t.id),
 'last_changed_at',(SELECT max(v.created_at) FROM v22_table_record_revisions v JOIN v22_table_records r ON r.id=v.record_id WHERE r.table_id=t.id),
 'latest_run_id',COALESCE((SELECT r.id FROM v22_runs r WHERE r.owner_id=t.owner_id AND r.definition->'output'->>'table_id'=t.id::text AND r.summary->>'committed'='true' ORDER BY r.finished_at DESC NULLS LAST,r.id DESC LIMIT 1),''))::text AS value
 FROM v22_data_tables t WHERE t.owner_id=? ORDER BY (SELECT max(r.last_observed_at) FROM v22_table_records r WHERE r.table_id=t.id) DESC NULLS LAST,t.created_at DESC,t.id DESC LIMIT ?`, owner, sectionLimit)
	if err != nil {
		return nil, err
	}
	h.RecentTables = []overview.Table{}
	for _, row := range rows {
		var t overview.Table
		if err = json.Unmarshal([]byte(row["value"]), &t); err != nil {
			return nil, err
		}
		h.RecentTables = append(h.RecentTables, t)
	}
	rows, err = query(s, "SELECT "+runBrief+`::text AS value FROM v22_runs r LEFT JOIN v22_data_tables t ON t.id::text=r.definition->'output'->>'table_id' AND t.owner_id=r.owner_id WHERE r.owner_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT ?`, owner, sectionLimit)
	if err != nil {
		return nil, err
	}
	h.RecentRuns = []overview.Run{}
	for _, row := range rows {
		var r overview.Run
		if err = json.Unmarshal([]byte(row["value"]), &r); err != nil {
			return nil, err
		}
		h.RecentRuns = append(h.RecentRuns, r)
	}
	if h.Counts["collectors"] == "0" && h.Counts["tables"] == "0" && h.Counts["active_runs"] == "0" && len(h.RecentRuns) == 0 {
		h.Status = "empty"
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return h, nil
}

// PositiveID is shared by API reads without accepting SQL identifiers.
func PositiveID(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, errors.New("positive collector ID required")
	}
	return n, nil
}
