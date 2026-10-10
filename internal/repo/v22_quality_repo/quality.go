package v22_quality_repo

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/quality"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"strconv"
	"time"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

var ErrNotFound = errors.New("quality resource not found")
var ErrConflict = errors.New("quality revision or recovery evidence changed")
var ErrInvalid = errors.New("invalid quality policy or baseline")

func session(ctx context.Context) *xorm.Session {
	return db.Instance().NewSession().Context(context.WithValue(ctx, xormlog.SessionShowSQLKey, false))
}
func raw(v any) string { b, _ := json.Marshal(v); return string(b) }
func guard(s *xorm.Session, owner, id int64) error {
	return idempotency.Lock(s, owner, "quality.collector", strconv.FormatInt(id, 10))
}
func collector(s *xorm.Session, owner, id int64) (*table.V22Collector, error) {
	c := new(table.V22Collector)
	has, e := s.Where("owner_id=? AND id=?", owner, id).Get(c)
	if e != nil {
		return nil, e
	}
	if !has {
		return nil, ErrNotFound
	}
	return c, nil
}

type PolicyView struct {
	Revision  int            `json:"revision"`
	Policy    quality.Policy `json:"policy"`
	Baseline  *quality.Facts `json:"baseline"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func policy(s *xorm.Session, owner, id int64) (PolicyView, error) {
	v := PolicyView{Policy: quality.Default()}
	p := new(table.V22QualityPolicy)
	has, e := s.Where("owner_id=? AND collector_id=?", owner, id).Get(p)
	if e != nil {
		return v, e
	}
	if has {
		v.Revision = p.Revision
		v.UpdatedAt = p.UpdatedAt
		if json.Unmarshal([]byte(p.Policy), &v.Policy) != nil {
			return v, ErrInvalid
		}
		if p.Baseline != "{}" {
			if json.Unmarshal([]byte(p.Baseline), &v.Baseline) != nil {
				return v, ErrInvalid
			}
		}
	}
	return v, nil
}
func GetPolicy(ctx context.Context, owner, id int64) (PolicyView, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := collector(s, owner, id); e != nil {
		return PolicyView{}, e
	}
	return policy(s, owner, id)
}

// Facts reads counts/field identities in SQL; record values and raw documents never leave the query.
func facts(s *xorm.Session, owner int64, runID string) (quality.Facts, error) {
	r := new(table.V22Run)
	has, e := s.Where("id=? AND owner_id=?", runID, owner).Omit("output_schema", "definition", "input", "summary").Get(r)
	if e != nil {
		return quality.Facts{}, e
	}
	if !has || r.FinishedAt == nil {
		return quality.Facts{}, ErrNotFound
	}
	rows, e := s.QueryString(`SELECT definition::text,output_schema::text,input::text,
 jsonb_build_object('committed',COALESCE((summary->>'committed')::boolean,false),'network_accessed',COALESCE((summary->>'network_accessed')::boolean,false),
 'pages',COALESCE((summary->>'pages')::int,0),'list_pages',COALESCE((summary->>'list_pages')::int,0),'reason',COALESCE(summary->>'stop_reason',''),
 'failed_step_id',COALESCE(summary->>'failed_step_id',''),'failed_stage',COALESCE(summary->>'failed_stage',''))::text AS brief,
 (SELECT jsonb_build_object('valid_records',count(*) FILTER(WHERE record->>'valid'='true'),'invalid_records',count(*) FILTER(WHERE record->>'valid'='false'),
 'field_keys',COALESCE((SELECT jsonb_agg(DISTINCT f->>'field_key') FROM v22_run_documents d CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.result->'extraction'->'records','[]')) x
 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(x->'fields','[]')) f WHERE d.run_id=r.id AND d.attempt=r.attempt AND f->>'valid'='false'
 AND ((d.result->>'step_id'=r.definition->'output'->>'step_id' AND d.result->>'stage'=r.definition->'output'->>'stage') OR (d.result->>'step_id'=r.summary->>'failed_step_id' AND d.result->>'stage'=r.summary->>'failed_stage'))),'[]'))
 FROM v22_run_documents d CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.result->'extraction'->'records','[]')) record
 WHERE d.run_id=r.id AND d.attempt=r.attempt AND d.result->>'step_id'=r.definition->'output'->>'step_id' AND d.result->>'stage'=r.definition->'output'->>'stage'
 AND (d.result->'output_indices' IS NULL OR d.result->'output_indices'='null' OR (d.result->'output_indices') @> jsonb_build_array((record->>'index')::int)))::text AS metrics
 FROM v22_runs r WHERE id=? AND owner_id=?`, runID, owner)
	if e != nil {
		return quality.Facts{}, e
	}
	if len(rows) != 1 {
		return quality.Facts{}, ErrNotFound
	}
	f := quality.Facts{RunID: r.Id, VersionID: r.VersionId, Status: r.Status, FinishedAt: *r.FinishedAt, FieldKeys: []string{}}
	f.TriggerSource = r.TriggerSource
	if r.StartedAt != nil {
		f.StartedAt = *r.StartedAt
	}
	if json.Unmarshal([]byte(rows[0]["brief"]), &f) != nil || json.Unmarshal([]byte(rows[0]["metrics"]), &f) != nil {
		return f, ErrInvalid
	}
	f.Key = quality.Comparable(rows[0]["definition"], rows[0]["output_schema"], rows[0]["input"], r.EntryType)
	return f, nil
}

type PolicyInput struct {
	ExpectedRevision int            `json:"expected_revision"`
	Policy           quality.Policy `json:"policy"`
	BaselineRunID    string         `json:"baseline_run_id"`
}

func SavePolicy(ctx context.Context, owner, id int64, i PolicyInput) (PolicyView, error) {
	if i.ExpectedRevision < 0 || i.Policy.Validate() != nil {
		return PolicyView{}, ErrInvalid
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return PolicyView{}, e
	}
	defer s.Rollback()
	if e := guard(s, owner, id); e != nil {
		return PolicyView{}, e
	}
	c, e := collector(s, owner, id)
	if e != nil {
		return PolicyView{}, e
	}
	if c.Status == "archived" {
		return PolicyView{}, ErrConflict
	}
	p, e := policy(s, owner, id)
	if e != nil {
		return p, e
	}
	if p.Revision != i.ExpectedRevision {
		return p, ErrConflict
	}
	var base *quality.Facts
	if i.BaselineRunID != "" {
		r := new(table.V22Run)
		has, e := s.Where("id=? AND owner_id=? AND collector_id=?", i.BaselineRunID, owner, id).Omit("definition", "input", "summary", "output_schema").Get(r)
		if e != nil {
			return p, e
		}
		if !has {
			return p, ErrNotFound
		}
		f, e := facts(s, owner, r.Id)
		if e != nil {
			return p, e
		}
		if !f.Complete() || f.Valid < 1 {
			return p, ErrInvalid
		}
		base = &f
	}
	now := time.Now()
	next := PolicyView{Revision: p.Revision + 1, Policy: i.Policy, Baseline: base, UpdatedAt: now}
	b := "{}"
	if base != nil {
		b = raw(base)
	}
	_, e = s.Exec(`INSERT INTO v22_quality_policies(collector_id,owner_id,revision,policy,baseline,updated_at) VALUES(?,?,?,?::jsonb,?::jsonb,?) ON CONFLICT(collector_id) DO UPDATE SET revision=EXCLUDED.revision,policy=EXCLUDED.policy,baseline=EXCLUDED.baseline,updated_at=EXCLUDED.updated_at`, id, owner, next.Revision, raw(i.Policy), b, now)
	if e != nil {
		return p, e
	}
	// A softer policy is not proof that old problems recovered.
	if _, e = s.Exec("UPDATE v22_quality_issues SET status='open',recovery_streak=0,recovery_run_id='',revision=revision+1,updated_at=? WHERE owner_id=? AND collector_id=? AND status<>'resolved'", now, owner, id); e != nil {
		return p, e
	}
	return next, s.Commit()
}
func issue(s *xorm.Session, owner int64, id string) (*table.V22QualityIssue, error) {
	r := new(table.V22QualityIssue)
	has, e := s.Where("id=? AND owner_id=?", id, owner).Get(r)
	if e != nil {
		return nil, e
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
func IssueDTO(i *table.V22QualityIssue) any {
	return map[string]any{"issue_id": i.Id, "collector_id": strconv.FormatInt(i.CollectorId, 10), "code": i.Code, "category": i.Category, "message": i.Message, "status": i.Status, "revision": i.Revision, "occurrences": i.Occurrences, "recovery_streak": i.RecoveryStreak, "first_run_id": i.FirstRunId, "last_run_id": i.LastRunId, "recovery_run_id": i.RecoveryRunId, "evidence": json.RawMessage(i.Evidence), "created_at": i.CreatedAt, "updated_at": i.UpdatedAt, "resolved_at": i.ResolvedAt}
}
func GetIssue(ctx context.Context, owner int64, id string) (any, error) {
	s := session(ctx)
	defer s.Close()
	i, e := issue(s, owner, id)
	if e != nil {
		return nil, e
	}
	return IssueDTO(i), nil
}
func ListIssues(ctx context.Context, owner, id int64, status, cursor string, limit int) (any, error) {
	if limit < 1 || limit > 50 {
		return nil, ErrInvalid
	}
	s := session(ctx)
	defer s.Close()
	var anchor *table.V22QualityIssue
	if cursor != "" {
		var err error
		anchor, err = issue(s, owner, cursor)
		if err != nil {
			return nil, err
		}
		if id > 0 && anchor.CollectorId != id {
			return nil, ErrNotFound
		}
	}
	q := s.Where("owner_id=?", owner)
	if id > 0 {
		q.And("collector_id=?", id)
	}
	if status == "active" {
		q.And("status<>'resolved'")
	} else if status != "all" {
		q.And("status=?", status)
	}
	if cursor != "" {
		q.And("(created_at,id)<(?,?)", anchor.CreatedAt, cursor)
	}
	rows := []table.V22QualityIssue{}
	if e := q.Desc("created_at", "id").Limit(limit + 1).Find(&rows); e != nil {
		return nil, e
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := []any{}
	next := ""
	for _, r := range rows {
		items = append(items, IssueDTO(&r))
	}
	if more {
		next = rows[len(rows)-1].Id
	}
	return map[string]any{"items": items, "next_cursor": next, "has_more": more}, nil
}
func Read(ctx context.Context, owner, id int64) (any, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	if _, e := s.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); e != nil {
		return nil, e
	}
	if _, e := collector(s, owner, id); e != nil {
		return nil, e
	}
	p, e := policy(s, owner, id)
	if e != nil {
		return nil, e
	}
	rows, e := s.QueryString(`SELECT e.run_id,e.facts::text,e.result::text,e.policy_revision FROM v22_quality_evaluations e JOIN v22_runs r ON r.id=e.run_id WHERE e.owner_id=? AND e.collector_id=? ORDER BY r.finished_at DESC,r.id DESC LIMIT 1`, owner, id)
	if e != nil {
		return nil, e
	}
	var latest any
	if len(rows) > 0 {
		latest = map[string]any{"run_id": rows[0]["run_id"], "facts": json.RawMessage(rows[0]["facts"]), "result": json.RawMessage(rows[0]["result"]), "policy_revision": rows[0]["policy_revision"]}
	}
	pending, e := s.Where("owner_id=? AND collector_id=? AND status NOT IN ('queued','running') AND NOT EXISTS(SELECT 1 FROM v22_quality_evaluations e WHERE e.run_id=v22_runs.id)", owner, id).Count(new(table.V22Run))
	if e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return map[string]any{"policy": p, "latest": latest, "pending_evaluations": pending, "as_of": time.Now()}, nil
}
func upsert(s *xorm.Session, owner, id int64, f quality.Finding, evidence quality.Facts) error {
	_, e := s.Exec(`INSERT INTO v22_quality_issues(id,owner_id,collector_id,code,category,message,status,first_run_id,last_run_id,evidence) VALUES(?,?,?,?,?,?,'open',?,?,?::jsonb)
 ON CONFLICT(owner_id,collector_id,code) DO UPDATE SET status='open',message=EXCLUDED.message,revision=v22_quality_issues.revision+1,occurrences=v22_quality_issues.occurrences+1,recovery_streak=0,recovery_run_id='',last_run_id=EXCLUDED.last_run_id,evidence=EXCLUDED.evidence,updated_at=NOW(),resolved_at=NULL`, uuid.NewString(), owner, id, f.Code, f.Category, f.Message, evidence.RunID, evidence.RunID, raw(evidence))
	return e
}
func scheduleHealthy(s *xorm.Session, owner, id int64, p quality.Policy) (bool, error) {
	rows, e := s.QueryString("SELECT enabled,next_at,last_decision FROM v22_schedules WHERE owner_id=? AND collector_id=?", owner, id)
	if e != nil {
		return false, e
	}
	if len(rows) == 0 {
		return true, nil
	}
	r := rows[0]
	if r["enabled"] != "true" && r["enabled"] != "t" {
		return false, nil
	}
	if r["last_decision"] == "disabled_invalid" || r["last_decision"] == "disabled_unavailable" || r["last_decision"] == "skipped_capacity" {
		return false, nil
	}
	check, e := s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND EXISTS(SELECT 1 FROM v22_schedules WHERE collector_id=? AND next_at>=NOW()-?*interval '1 minute')", id, id, p.ScheduleGraceMinutes)
	return len(check) > 0, e
}

// ProcessOne serializes the observer, not the formal writer. Evidence and issue
// transitions commit together; crashes/restarts never count the same run twice.
func ProcessOne(ctx context.Context) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	if e := idempotency.Lock(s, 0, "quality.observer", "singleton"); e != nil {
		return e
	}
	rows, e := s.QueryString(`SELECT r.id,r.owner_id,r.collector_id FROM v22_runs r WHERE r.status NOT IN ('queued','running') AND r.finished_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM v22_quality_evaluations e WHERE e.run_id=r.id) ORDER BY r.finished_at,r.id LIMIT 1`)
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		return s.Commit()
	}
	owner, _ := strconv.ParseInt(rows[0]["owner_id"], 10, 64)
	id, _ := strconv.ParseInt(rows[0]["collector_id"], 10, 64)
	if e = guard(s, owner, id); e != nil {
		return e
	}
	p, e := policy(s, owner, id)
	if e != nil {
		return e
	}
	f, e := facts(s, owner, rows[0]["id"])
	if e != nil {
		return e
	}
	previous := 0
	prev, e := s.QueryString(`SELECT e.result->>'failure_streak' AS streak FROM v22_quality_evaluations e JOIN v22_runs r ON r.id=e.run_id WHERE e.owner_id=? AND e.collector_id=? AND e.policy_revision=? ORDER BY r.finished_at DESC,r.id DESC LIMIT 1`, owner, id, p.Revision)
	if e != nil {
		return e
	}
	if len(prev) > 0 {
		previous, _ = strconv.Atoi(prev[0]["streak"])
	}
	result := quality.Evaluate(p.Policy, f, p.Baseline, previous)
	c, e := collector(s, owner, id)
	if e != nil {
		return e
	}
	ignored := c.Status == "archived" || (!p.UpdatedAt.IsZero() && f.FinishedAt.Before(p.UpdatedAt))
	if ignored {
		result = quality.Result{Findings: []quality.Finding{}, BaselineStatus: "policy_changed", FailureStreak: 0}
	}
	if _, e = s.InsertOne(&table.V22QualityEvaluation{RunId: f.RunID, OwnerId: owner, CollectorId: id, PolicyRevision: p.Revision, Facts: raw(f), Result: raw(result), CreatedAt: time.Now()}); e != nil {
		return e
	}
	if ignored {
		return s.Commit()
	}
	for _, finding := range result.Findings {
		if e = upsert(s, owner, id, finding, f); e != nil {
			return e
		}
	}
	healthy, e := scheduleHealthy(s, owner, id, p.Policy)
	if e != nil {
		return e
	}
	if result.RecoveryEligible && c.PublishedVersionId != nil && *c.PublishedVersionId == f.VersionID {
		if _, e = s.Exec(`UPDATE v22_quality_issues SET status='open',recovery_streak=0,recovery_run_id='',revision=revision+1,updated_at=NOW() WHERE owner_id=? AND collector_id=? AND status<>'resolved' AND category<>'schedule' AND COALESCE(evidence->>'comparison_key','')<>?`, owner, id, f.Key); e != nil {
			return e
		}
		clause := ""
		if !healthy || f.TriggerSource != "schedule" {
			clause = " AND category<>'schedule'"
		}
		_, e = s.Exec(`UPDATE v22_quality_issues SET recovery_streak=recovery_streak+1,recovery_run_id=?,revision=revision+1,updated_at=NOW(),status=CASE WHEN recovery_streak+1>=? THEN 'ready' ELSE 'open' END WHERE owner_id=? AND collector_id=? AND status<>'resolved' AND ? > COALESCE((evidence->>'finished_at')::timestamptz,created_at) AND (category='schedule' OR evidence->>'comparison_key'=?)`+clause, f.RunID, p.Policy.RecoveryRuns, owner, id, f.StartedAt, f.Key)
	} else if f.Status != "cancelled" {
		_, e = s.Exec("UPDATE v22_quality_issues SET status='open',recovery_streak=0,recovery_run_id='',revision=revision+1,updated_at=NOW() WHERE owner_id=? AND collector_id=? AND status<>'resolved'", owner, id)
	}
	if e != nil {
		return e
	}
	return s.Commit()
}

type ResolveInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	RecoveryRunID    string `json:"recovery_run_id"`
	Confirmed        bool   `json:"confirmed"`
}

func Resolve(ctx context.Context, owner int64, issueID string, i ResolveInput) (any, error) {
	if !i.Confirmed || i.ExpectedRevision < 1 || i.RecoveryRunID == "" {
		return nil, ErrInvalid
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	r, e := issue(s, owner, issueID)
	if e != nil {
		return nil, e
	}
	if e = guard(s, owner, r.CollectorId); e != nil {
		return nil, e
	}
	r, e = issue(s, owner, issueID)
	if e != nil {
		return nil, e
	}
	if r.Status == "resolved" && r.RecoveryRunId == i.RecoveryRunID {
		return IssueDTO(r), nil
	}
	if r.Revision != i.ExpectedRevision || r.Status != "ready" || r.RecoveryRunId != i.RecoveryRunID {
		return nil, ErrConflict
	}
	if r.Category == "schedule" {
		// Schedule save/tick lock the schedule before the collector. Keep that
		// order and prevent a concurrent pause from invalidating confirmation.
		if _, e = s.QueryString("SELECT collector_id FROM v22_schedules WHERE owner_id=? AND collector_id=? FOR UPDATE", owner, r.CollectorId); e != nil {
			return nil, e
		}
	}
	if _, e = s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", r.CollectorId, owner); e != nil {
		return nil, e
	}
	c, e := collector(s, owner, r.CollectorId)
	if e != nil {
		return nil, e
	}
	p, e := policy(s, owner, r.CollectorId)
	if e != nil {
		return nil, e
	}
	f, e := facts(s, owner, i.RecoveryRunID)
	if e != nil {
		return nil, e
	}
	pending, e := s.Where("owner_id=? AND collector_id=? AND (status IN ('queued','running') OR NOT EXISTS(SELECT 1 FROM v22_quality_evaluations e WHERE e.run_id=v22_runs.id))", owner, r.CollectorId).Count(new(table.V22Run))
	if e != nil {
		return nil, e
	}
	latest, e := s.QueryString("SELECT id FROM v22_runs WHERE owner_id=? AND collector_id=? AND status<>'cancelled' ORDER BY finished_at DESC NULLS LAST,id DESC LIMIT 1", owner, r.CollectorId)
	if e != nil {
		return nil, e
	}
	healthy, e := scheduleHealthy(s, owner, r.CollectorId, p.Policy)
	if e != nil {
		return nil, e
	}
	var original quality.Facts
	if json.Unmarshal([]byte(r.Evidence), &original) != nil || r.Category != "schedule" && original.Key != f.Key || !f.StartedAt.After(original.FinishedAt) {
		return nil, ErrConflict
	}
	if c.Status == "archived" || !p.Policy.Enabled || pending > 0 || len(latest) != 1 || latest[0]["id"] != f.RunID || c.PublishedVersionId == nil || *c.PublishedVersionId != f.VersionID || !quality.Evaluate(p.Policy, f, p.Baseline, 0).RecoveryEligible || r.RecoveryStreak < p.Policy.RecoveryRuns || (r.Category == "schedule" && (!healthy || f.TriggerSource != "schedule")) {
		return nil, ErrConflict
	}
	now := time.Now()
	r.Status = "resolved"
	r.ResolvedAt = &now
	r.UpdatedAt = now
	r.Revision++
	if _, e = s.ID(r.Id).Cols("status", "resolved_at", "updated_at", "revision").Update(r); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return IssueDTO(r), nil
}
