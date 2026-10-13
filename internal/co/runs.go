package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"time"
	"xorm.io/xorm"
)

type Run struct {
	ID              string `xorm:"id pk"`
	OwnerID         int64  `xorm:"owner_id"`
	TaskVersionID   string `xorm:"task_version_id"`
	Manifest        string `xorm:"jsonb manifest"`
	Namespace       string
	Status          string
	ControlEpoch    int64
	PreparingCursor int
	InputCount      int
	OutputCount     int
	EventSeq        int64
	RetryOf         string
	RetryScope      string `xorm:"jsonb retry_scope"`
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

func (Run) TableName() string { return "co_runs" }

type WorkItem struct {
	ID           string `xorm:"id pk"`
	RunID        string `xorm:"run_id"`
	StageRunID   string `xorm:"stage_run_id"`
	InputID      string `xorm:"input_id"`
	SourceKey    string
	InputValues  string `xorm:"jsonb input_values"`
	InputHash    string
	Status       string
	Attempt      int
	NextRetryAt  *time.Time
	LeaseToken   string
	LeaseUntil   *time.Time
	ControlEpoch int64
	Checkpoint   string `xorm:"jsonb checkpoint"`
	Error        string `xorm:"jsonb error"`
	CreatedAt    time.Time
}

func (WorkItem) TableName() string { return "co_work_items" }

type StageRun struct {
	ID                 string `xorm:"id pk"`
	RunID              string `xorm:"run_id"`
	StageID            string `xorm:"stage_id"`
	CollectorVersionID string `xorm:"collector_version_id"`
	InputClosed        bool
	CloseReason        string
}

func (StageRun) TableName() string { return "co_stage_runs" }

type RunInput struct {
	ID             string `xorm:"id pk"`
	RunID          string `xorm:"run_id"`
	Ordinal        int
	SourceKey      string
	Values         string `xorm:"jsonb values_json"`
	Source         string `xorm:"jsonb source"`
	MaterializedAt *time.Time
}

func (RunInput) TableName() string { return "co_run_inputs" }

type Attempt struct {
	ID          string `xorm:"id pk"`
	RunID       string `xorm:"run_id"`
	ItemID      string `xorm:"item_id"`
	Number      int
	LeaseToken  string
	SessionID   string `xorm:"session_id"`
	Status      string
	CommandRefs string `xorm:"jsonb command_refs"`
	Error       string `xorm:"jsonb error"`
	StartedAt   time.Time
	FinishedAt  *time.Time
}

func (Attempt) TableName() string { return "co_work_attempts" }

type WorkClaim struct {
	Run      Run
	Item     WorkItem
	Attempt  Attempt
	Manifest TaskManifest
	Recovery bool
}

func getRun(s *xorm.Session, owner int64, id string, lock bool) (*Run, error) {
	r := new(Run)
	q := "SELECT * FROM co_runs WHERE id=?"
	args := []any{id}
	if owner > 0 {
		q += " AND owner_id=?"
		args = append(args, owner)
	}
	if lock {
		q += " FOR UPDATE"
	}
	ok, e := s.SQL(q, args...).Get(r)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}
func event(s *xorm.Session, r *Run, kind, item string, value any) error {
	r.EventSeq++
	raw := collector.JSON(value)
	if len(raw) > 8192 {
		return collector.Invalid("event", "事件元数据超过8KiB")
	}
	_, e := s.Exec("UPDATE co_runs SET event_seq=? WHERE id=?", r.EventSeq, r.ID)
	if e != nil {
		return e
	}
	var itemID any
	if item != "" {
		itemID = item
	}
	_, e = s.Exec("INSERT INTO co_run_events(run_id,sequence,type,item_id,data) VALUES(?,?,?,?,CAST(? AS jsonb))", r.ID, r.EventSeq, kind, itemID, raw)
	return e
}
func newRun(s *xorm.Session, owner int64, v TaskVersion, namespace, retryOf string, inputs []RunInput, scope []string) (any, error) {
	var m TaskManifest
	if e := collector.Strict([]byte(v.Manifest), &m); e != nil {
		return nil, e
	}
	if len(inputs) == 0 {
		return nil, collector.Invalid("inputs", "没有可执行输入")
	}
	if _, e := buildManifest(s, owner, m.Definition); e != nil {
		return nil, e
	}
	run := Run{ID: uuid.NewString(), OwnerID: owner, TaskVersionID: v.ID, Manifest: v.Manifest, Namespace: namespace, Status: "preparing", InputCount: len(inputs), RetryOf: retryOf, RetryScope: collector.JSON(scope), CreatedAt: time.Now()}
	// Optional FK is SQL NULL, not an empty UUID-shaped reference.
	var retry any
	if retryOf != "" {
		retry = retryOf
	}
	if _, e := s.Exec(`INSERT INTO co_runs(id,owner_id,task_version_id,manifest,namespace,status,input_count,retry_of,retry_scope) VALUES(?,?,?,CAST(? AS jsonb),?,'preparing',?,?,CAST(? AS jsonb))`, run.ID, owner, v.ID, v.Manifest, namespace, len(inputs), retry, run.RetryScope); e != nil {
		return nil, e
	}
	stage := StageRun{ID: uuid.NewString(), RunID: run.ID, StageID: m.Definition.EntryStage, CollectorVersionID: m.Collector.ID}
	if _, e := s.Insert(&stage); e != nil {
		return nil, e
	}
	for i, input := range inputs {
		input.ID = uuid.NewString()
		input.RunID = run.ID
		input.Ordinal = i
		input.MaterializedAt = nil
		if _, e := s.Insert(&input); e != nil {
			return nil, e
		}
	}
	if e := event(s, &run, "preparing", "", map[string]any{"input_count": len(inputs), "namespace": namespace, "retry_of": retryOf}); e != nil {
		return nil, e
	}
	return map[string]any{"id": run.ID, "status": run.Status, "namespace": namespace, "input_count": len(inputs)}, nil
}
func StartTask(ctx context.Context, owner int64, taskID, key, versionID, namespace string) (*Operation, error) {
	if namespace != "formal" && namespace != "trial" {
		return nil, collector.Invalid("namespace", "请选择formal/trial")
	}
	return Mutate(ctx, owner, "startTask", taskID, key, map[string]string{"task_version_id": versionID, "namespace": namespace}, false, func(s *xorm.Session, o *Operation) (any, error) {
		if _, e := getTask(s, owner, taskID, true); e != nil {
			return nil, e
		}
		v := new(TaskVersion)
		ok, e := s.Where("owner_id=? AND task_id=? AND id=?", owner, taskID, versionID).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		active, e := views(s, `SELECT r.id,r.status FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND r.namespace=? AND r.status IN ('preparing','running','pausing','paused','awaiting_confirmation') LIMIT 1`, taskID, namespace)
		if e != nil {
			return nil, e
		}
		if len(active) > 0 {
			return map[string]any{"skipped": true, "active_run": active[0]}, nil
		}
		var m TaskManifest
		if e = collector.Strict([]byte(v.Manifest), &m); e != nil {
			return nil, e
		}
		inputs := []RunInput{}
		for i, values := range m.Definition.Source.Inputs {
			inputs = append(inputs, RunInput{SourceKey: fmt.Sprintf("root:%d:%s", i, collector.Hash(values)), Values: collector.JSON(values), Source: collector.JSON(map[string]any{"kind": "manual", "ordinal": i, "task_version_id": v.ID})})
		}
		return newRun(s, owner, *v, namespace, "", inputs, []string{})
	})
}
func RetryFailed(ctx context.Context, owner int64, id, key string, confirmed bool) (*Operation, error) {
	if !confirmed {
		return nil, collector.Invalid("confirmed", "补采会再次访问网站并按原绑定保存，请确认")
	}
	return Mutate(ctx, owner, "retryFailed", id, key, map[string]bool{"confirmed": confirmed}, false, func(s *xorm.Session, o *Operation) (any, error) {
		original, e := getRun(s, owner, id, false)
		if e != nil {
			return nil, e
		}
		v := new(TaskVersion)
		ok, e := s.Where("owner_id=? AND id=?", owner, original.TaskVersionID).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		if _, e = getTask(s, owner, v.TaskID, true); e != nil {
			return nil, e
		}
		original, e = getRun(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if !terminal(original.Status) {
			return nil, conflict("请先结束原运行；未知动作需确认，不能按失败自动补采")
		}
		count, e := s.SQL(`SELECT COUNT(*) FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND r.namespace=? AND r.status IN ('preparing','running','pausing','paused','awaiting_confirmation')`, v.TaskID, original.Namespace).Count(new(Run))
		if e != nil {
			return nil, e
		}
		if count > 0 {
			return nil, conflict("任务已有活动运行")
		}
		items := []WorkItem{}
		if e = s.Where("run_id=? AND status='failed'", id).OrderBy("created_at,id").Limit(100).Find(&items); e != nil {
			return nil, e
		}
		inputs := []RunInput{}
		scope := []string{}
		for _, item := range items {
			inputs = append(inputs, RunInput{SourceKey: item.SourceKey, Values: item.InputValues, Source: collector.JSON(map[string]any{"kind": "retry", "run_id": id, "item_id": item.ID, "input_id": item.InputID})})
			scope = append(scope, item.ID)
		}
		return newRun(s, owner, *v, original.Namespace, id, inputs, scope)
	})
}
func terminal(status string) bool {
	switch status {
	case "succeeded", "failed", "partial", "limited", "cancelled":
		return true
	}
	return false
}
func settle(s *xorm.Session, r *Run) error {
	if terminal(r.Status) {
		return nil
	}
	rows, e := s.QueryString("SELECT status,COUNT(*) AS count FROM co_work_items WHERE run_id=? GROUP BY status", r.ID)
	if e != nil {
		return e
	}
	counts := map[string]int{}
	for _, row := range rows {
		var n int
		fmt.Sscan(row["count"], &n)
		counts[row["status"]] = n
	}
	next := r.Status
	if counts["awaiting_confirmation"] > 0 {
		next = "awaiting_confirmation"
	} else if r.Status == "pausing" && counts["running"] == 0 {
		next = "paused"
	} else if r.Status == "running" || r.Status == "preparing" {
		stage := new(StageRun)
		ok, e := s.Where("run_id=?", r.ID).Get(stage)
		if e != nil {
			return e
		}
		if ok && stage.InputClosed && counts["running"]+counts["queued"] == 0 {
			switch {
			case counts["failed"] > 0:
				next = "failed"
				if counts["succeeded"]+counts["partial"]+counts["limited"] > 0 || r.OutputCount > 0 {
					next = "partial"
				}
			case counts["partial"] > 0:
				next = "partial"
			case counts["limited"] > 0:
				next = "limited"
			default:
				next = "succeeded"
			}
		}
	}
	if _, e = s.Exec("UPDATE co_stage_runs SET status=?,counters=CAST(? AS jsonb) WHERE run_id=?", next, collector.JSON(counts), r.ID); e != nil {
		return e
	}
	if next == r.Status {
		return nil
	}
	r.Status = next
	var finished any
	if terminal(next) {
		finished = time.Now()
	}
	if _, e = s.Exec("UPDATE co_runs SET status=?,finished_at=? WHERE id=?", next, finished, r.ID); e != nil {
		return e
	}
	return event(s, r, next, "", map[string]any{"items": counts, "outputs": r.OutputCount})
}

func canonicalHash(raw string) string {
	var value any
	if collector.Strict([]byte(raw), &value) != nil {
		return ""
	}
	return collector.Hash(value)
}
func unresolvedCommands(refs string) []string {
	var rows []struct {
		CommandID string `json:"command_id"`
		Status    string `json:"status"`
	}
	_ = json.Unmarshal([]byte(refs), &rows)
	last := map[string]string{}
	for _, row := range rows {
		last[row.CommandID] = row.Status
	}
	pending := []string{}
	for id, status := range last {
		if status == "pending" {
			pending = append(pending, id)
		}
	}
	return pending
}
func safeBrowserResume(s *xorm.Session, item *WorkItem, m TaskManifest, cp collector.Checkpoint) bool {
	d := m.CollectorDefinition
	if len(d.Actions) > 0 || cp.Pages == 0 || cp.NextURL == "" || d.Pagination.Resume != "url_or_cursor" || (d.Pagination.Kind != "url" && d.Pagination.Kind != "cursor") {
		return false
	}
	a := new(Attempt)
	ok, e := s.Where("item_id=? AND number=?", item.ID, item.Attempt).Get(a)
	return e == nil && ok && len(unresolvedCommands(a.CommandRefs)) == 0
}
func ControlRun(ctx context.Context, owner int64, id, key, action string, expectedEpoch ...int64) (*Operation, error) {
	if action != "pause" && action != "resume" && action != "cancel" {
		return nil, collector.Invalid("action", "需要pause/resume/cancel")
	}
	payload := map[string]any{"action": action}
	if len(expectedEpoch) > 0 {
		payload["expected_control_epoch"] = expectedEpoch[0]
	}
	return Mutate(ctx, owner, "controlRun", id, key, payload, false, func(s *xorm.Session, o *Operation) (any, error) {
		r, e := getRun(s, owner, id, true)
		if e != nil {
			return nil, e
		}
		if len(expectedEpoch) > 0 && expectedEpoch[0] != r.ControlEpoch {
			return nil, conflict("运行控制状态已变化，请刷新后确认")
		}
		if terminal(r.Status) {
			return map[string]any{"id": id, "status": r.Status}, nil
		}
		switch action {
		case "pause":
			if r.Status != "running" && r.Status != "preparing" {
				return nil, conflict("当前状态不能暂停")
			}
			r.Status = "pausing"
			r.ControlEpoch++
		case "resume":
			if r.Status != "paused" {
				return nil, conflict("仅paused可恢复；未知动作需逐项确认")
			}
			r.Status = "preparing"
			r.ControlEpoch++
		case "cancel":
			r.Status = "cancelled"
			r.ControlEpoch++
			if _, e = s.Exec("UPDATE co_work_items SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE run_id=? AND status IN ('queued','running','awaiting_confirmation')", id); e != nil {
				return nil, e
			}
			if _, e = s.Exec("UPDATE co_work_attempts SET status='cancelled',finished_at=NOW() WHERE run_id=? AND status='running'", id); e != nil {
				return nil, e
			}
			if _, e = s.Exec("UPDATE co_stage_runs SET status='cancelled',input_closed=TRUE,close_reason='operator_cancelled' WHERE run_id=?", id); e != nil {
				return nil, e
			}
		}
		if _, e = s.Exec("UPDATE co_runs SET status=?,control_epoch=?,finished_at=CASE WHEN ?='cancelled' THEN NOW() ELSE NULL END WHERE id=?", r.Status, r.ControlEpoch, r.Status, id); e != nil {
			return nil, e
		}
		if e = event(s, r, action, "", map[string]any{"control_epoch": r.ControlEpoch}); e != nil {
			return nil, e
		}
		if e = settle(s, r); e != nil {
			return nil, e
		}
		return map[string]any{"id": id, "status": r.Status}, nil
	})
}

// prepare and claim always lock run  ->  stage  ->  item. No remote work in these transactions.
func PrepareRuns(ctx context.Context) error {
	s := session(ctx)
	defer s.Close()
	ids, e := s.QueryString("SELECT id FROM co_runs WHERE status IN ('preparing','running') ORDER BY created_at LIMIT 20")
	if e != nil {
		return e
	}
	for _, row := range ids {
		if e := prepareRun(ctx, row["id"]); e != nil {
			return e
		}
	}
	return nil
}
func prepareRun(ctx context.Context, id string) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	r, e := getRun(s, 0, id, true)
	if e != nil {
		return e
	}
	if r.Status != "preparing" && r.Status != "running" {
		return s.Commit()
	}
	var m TaskManifest
	if e = collector.Strict([]byte(r.Manifest), &m); e != nil {
		return e
	}
	if time.Since(r.CreatedAt) > time.Duration(m.Definition.Budget.MaxDurationMS)*time.Millisecond {
		if _, e = s.Exec("UPDATE co_work_items SET status='limited',lease_token=NULL,lease_until=NULL WHERE run_id=? AND status IN ('queued','running')", id); e != nil {
			return e
		}
		r.Status = "limited"
		if _, e = s.Exec("UPDATE co_runs SET status='limited',finished_at=NOW() WHERE id=?", id); e != nil {
			return e
		}
		if _, e = s.Exec("UPDATE co_work_attempts SET status='limited',finished_at=NOW() WHERE run_id=? AND status='running'", id); e != nil {
			return e
		}
		if _, e = s.Exec("UPDATE co_stage_runs SET status='limited',input_closed=TRUE,close_reason='duration_budget' WHERE run_id=?", id); e != nil {
			return e
		}
		if e = event(s, r, "limited", "", map[string]string{"reason": "DURATION_BUDGET"}); e != nil {
			return e
		}
		return s.Commit()
	}
	stage := new(StageRun)
	ok, e := s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? FOR UPDATE", id).Get(stage)
	if e != nil {
		return e
	}
	if !ok {
		return ErrNotFound
	}
	count, e := s.Where("run_id=? AND status IN ('queued','running')", id).Count(new(WorkItem))
	if e != nil {
		return e
	}
	capacity := m.Definition.Budget.MaxPending - int(count)
	if capacity > 10 {
		capacity = 10
	}
	if capacity > 0 {
		inputs := []RunInput{}
		if e = s.Where("run_id=? AND ordinal>=?", id, r.PreparingCursor).OrderBy("ordinal").Limit(capacity).Find(&inputs); e != nil {
			return e
		}
		for _, in := range inputs {
			item := WorkItem{ID: uuid.NewString(), RunID: id, StageRunID: stage.ID, InputID: in.ID, SourceKey: in.SourceKey, InputValues: in.Values, InputHash: canonicalHash(in.Values), Status: "queued", Checkpoint: "{}", Error: "{}", CreatedAt: time.Now()}
			if _, e = s.Exec(`INSERT INTO co_work_items(id,run_id,stage_run_id,input_id,source_key,input_values,input_hash) VALUES(?,?,?,?,?,CAST(? AS jsonb),?) ON CONFLICT(run_id,stage_run_id,source_key) DO NOTHING`, item.ID, id, stage.ID, in.ID, in.SourceKey, in.Values, item.InputHash); e != nil {
				return e
			}
			if _, e = s.Exec("UPDATE co_run_inputs SET materialized_at=COALESCE(materialized_at,NOW()) WHERE id=?", in.ID); e != nil {
				return e
			}
			r.PreparingCursor = in.Ordinal + 1
		}
		if _, e = s.Exec("UPDATE co_runs SET preparing_cursor=? WHERE id=?", r.PreparingCursor, id); e != nil {
			return e
		}
		if len(inputs) > 0 {
			if e = event(s, r, "inputs_materialized", "", map[string]int{"prepared": r.PreparingCursor, "total": r.InputCount}); e != nil {
				return e
			}
		}
	}
	if r.PreparingCursor == r.InputCount {
		if _, e = s.Exec("UPDATE co_stage_runs SET input_closed=TRUE,close_reason='manual_snapshot_complete' WHERE id=?", stage.ID); e != nil {
			return e
		}
		r.Status = "running"
		if _, e = s.Exec("UPDATE co_runs SET status='running' WHERE id=?", id); e != nil {
			return e
		}
	}
	if e = settle(s, r); e != nil {
		return e
	}
	return s.Commit()
}
func ClaimWork(ctx context.Context, browser bool) (*WorkClaim, error) {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	r := new(Run)
	ok, e := s.SQL(`SELECT r.* FROM co_runs r WHERE r.status IN ('preparing','running','pausing','awaiting_confirmation') AND ((r.manifest -> 'collector_definition' -> 'fetch' ->> 'kind'='browser')=?) AND EXISTS(SELECT 1 FROM co_work_items w WHERE w.run_id=r.id AND ((r.status IN ('preparing','running') AND w.status='queued' AND (r.manifest -> 'collector_definition' -> 'fetch' ->> 'kind'<>'browser' OR (SELECT COUNT(*) FROM co_work_items active WHERE active.run_id=r.id AND active.status='running')<CAST(r.manifest -> 'definition' -> 'budget' ->> 'browser_slots' AS INT)) AND (w.next_retry_at IS NULL OR w.next_retry_at<=NOW())) OR (w.status='running' AND w.lease_until<NOW()))) ORDER BY r.created_at FOR UPDATE SKIP LOCKED LIMIT 1`, browser).Get(r)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, s.Commit()
	}
	stage := new(StageRun)
	if _, e = s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? FOR UPDATE", r.ID).Get(stage); e != nil {
		return nil, e
	}
	item := new(WorkItem)
	ok, e = s.SQL(`SELECT * FROM co_work_items WHERE run_id=? AND ((? IN ('preparing','running') AND status='queued' AND (next_retry_at IS NULL OR next_retry_at<=NOW())) OR (status='running' AND lease_until<NOW())) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, r.ID, r.Status).Get(item)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, s.Commit()
	}
	var m TaskManifest
	if e = collector.Strict([]byte(r.Manifest), &m); e != nil {
		return nil, e
	}
	if item.Status == "running" {
		status := "queued"
		code := "LEASE_EXPIRED"
		var cp collector.Checkpoint
		_ = json.Unmarshal([]byte(item.Checkpoint), &cp)
		if cp.Done {
			status = cp.FinalStatus
			code = cp.FinalReason
		} else if (m.CollectorDefinition.Fetch.Kind == "browser" && !safeBrowserResume(s, item, m, cp)) || m.CollectorDefinition.Fetch.Method == "POST" {
			status = "awaiting_confirmation"
			code = "OUTCOME_UNCERTAIN"
		} else if item.Attempt >= 3 {
			status = "failed"
		}
		problem := collector.Fault(code, "worker", "原尝试租约已过期；已提交结果保留，浏览器/POST不会自动重放")
		if _, e = s.Exec("UPDATE co_work_attempts SET status=?,error=CAST(? AS jsonb),finished_at=NOW() WHERE item_id=? AND status='running'", status, collector.JSON(problem), item.ID); e != nil {
			return nil, e
		}
		if _, e = s.Exec("UPDATE co_work_items SET status=?,error=CAST(? AS jsonb),lease_token=NULL,lease_until=NULL WHERE id=?", status, collector.JSON(problem), item.ID); e != nil {
			return nil, e
		}
		if e = event(s, r, "lease_recovered", item.ID, map[string]string{"status": status, "code": code}); e != nil {
			return nil, e
		}
		if e = settle(s, r); e != nil {
			return nil, e
		}
		if e = s.Commit(); e != nil {
			return nil, e
		}
		if status == "awaiting_confirmation" {
			a := Attempt{}
			read := session(ctx)
			defer read.Close()
			ok, e := read.Where("item_id=? AND number=?", item.ID, item.Attempt).Get(&a)
			if e != nil {
				return nil, e
			}
			if ok {
				return &WorkClaim{Run: *r, Item: *item, Attempt: a, Manifest: m, Recovery: true}, nil
			}
		}
		return nil, nil
	}
	// Global worker pool is bounded; each task adds its own browser slot limit.
	if m.CollectorDefinition.Fetch.Kind == "browser" {
		n, e := s.Where("run_id=? AND status='running'", r.ID).Count(new(WorkItem))
		if e != nil {
			return nil, e
		}
		if n >= int64(m.Definition.Budget.BrowserSlots) {
			return nil, s.Commit()
		}
	}
	item.Attempt++
	item.LeaseToken = uuid.NewString()
	item.ControlEpoch = r.ControlEpoch
	item.Status = "running"
	until := time.Now().Add(30 * time.Second)
	item.LeaseUntil = &until
	if _, e = s.ID(item.ID).Cols("attempt", "lease_token", "control_epoch", "status", "lease_until").Update(item); e != nil {
		return nil, e
	}
	attempt := Attempt{ID: uuid.NewString(), RunID: r.ID, ItemID: item.ID, Number: item.Attempt, LeaseToken: item.LeaseToken, SessionID: uuid.NewString(), Status: "running", CommandRefs: "[]", Error: "{}", StartedAt: time.Now()}
	if _, e = s.Insert(&attempt); e != nil {
		return nil, e
	}
	if e = event(s, r, "item_started", item.ID, map[string]any{"attempt": item.Attempt}); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return &WorkClaim{Run: *r, Item: *item, Attempt: attempt, Manifest: m}, nil
}
func fence(r *Run, item *WorkItem, c *WorkClaim) error {
	if item.Status != "running" || item.LeaseToken != c.Item.LeaseToken || item.LeaseUntil == nil || !item.LeaseUntil.After(time.Now()) {
		return collector.Fault("LEASE_LOST", "worker", "租约已失效，当前尝试不能提交")
	}
	if r.Status != "running" && r.Status != "preparing" && r.Status != "pausing" {
		return collector.Fault("CONTROL_STOPPED", "run", "运行已停止或等待确认")
	}
	epoch := r.ControlEpoch
	if r.Status == "pausing" {
		epoch--
	}
	if epoch != item.ControlEpoch {
		return collector.Fault("CONTROL_CHANGED", "run", "运行控制代次已变化")
	}
	return nil
}
func lockClaim(s *xorm.Session, c *WorkClaim) (*Run, *WorkItem, error) {
	r, e := getRun(s, c.Run.OwnerID, c.Run.ID, true)
	if e != nil {
		return nil, nil, e
	}
	stage := new(StageRun)
	if _, e = s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? AND id=? FOR UPDATE", r.ID, c.Item.StageRunID).Get(stage); e != nil {
		return nil, nil, e
	}
	item := new(WorkItem)
	ok, e := s.SQL("SELECT * FROM co_work_items WHERE run_id=? AND id=? AND lease_until>NOW() FOR UPDATE", r.ID, c.Item.ID).Get(item)
	if e != nil {
		return nil, nil, e
	}
	if !ok {
		return nil, nil, collector.Fault("LEASE_LOST", "worker", "工作项不存在或数据库租约已到期")
	}
	return r, item, fence(r, item, c)
}
func Heartbeat(ctx context.Context, c *WorkClaim) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	r, item, e := lockClaim(s, c)
	if e != nil {
		return e
	}
	_ = r
	_, e = s.Exec("UPDATE co_work_items SET lease_until=NOW()+INTERVAL '30 seconds' WHERE id=?", item.ID)
	if e != nil {
		return e
	}
	return s.Commit()
}
func JournalCommand(ctx context.Context, c *WorkClaim, command string, resolved bool) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	_, _, e := lockClaim(s, c)
	if e != nil {
		return e
	}
	status := "pending"
	if resolved {
		status = "resolved"
	}
	if _, e = s.Exec(`UPDATE co_work_attempts SET command_refs=command_refs || CAST(? AS jsonb) WHERE id=?`, collector.JSON([]any{map[string]string{"command_id": command, "status": status}}), c.Attempt.ID); e != nil {
		return e
	}
	return s.Commit()
}
func FinishWork(ctx context.Context, c *WorkClaim, result collector.Result) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	r, item, e := lockClaim(s, c)
	if e != nil {
		return e
	}
	status := result.Status
	if status == "" {
		status = "failed"
	}
	if result.Error != nil && status != "awaiting_confirmation" {
		status = "failed"
	}
	if result.Error != nil && result.Error.Code == "WORKER_PANIC" && (c.Manifest.CollectorDefinition.Fetch.Kind == "browser" || c.Manifest.CollectorDefinition.Fetch.Method == "POST") {
		status = "awaiting_confirmation"
	}
	var checkpoint collector.Checkpoint
	_ = json.Unmarshal([]byte(item.Checkpoint), &checkpoint)
	if checkpoint.Done && (result.Error == nil || result.Error.Code == "BATCH_COMMIT_FAILED") {
		status = checkpoint.FinalStatus
		result.Error = nil
		result.Reason = checkpoint.FinalReason
	}
	var invalidCount int64
	invalidCount, e = s.Where("item_id=? AND decision IN ('invalid','conflict')", item.ID).Table("co_observations").Count()
	if e != nil {
		return e
	}
	if invalidCount > 0 && status == "succeeded" {
		status = "partial"
	}
	if result.Reason == "OUTPUT_BUDGET" {
		status = "limited"
	}
	nextRetry := any(nil)
	canRetry := result.Error != nil && retryableRead(c.Manifest.CollectorDefinition, result.Error)
	if result.Error != nil && result.Error.Code == "BATCH_COMMIT_FAILED" && c.Manifest.CollectorDefinition.Fetch.Kind == "browser" {
		d := c.Manifest.CollectorDefinition
		if len(d.Actions) == 0 && d.Pagination.Resume == "url_or_cursor" && (d.Pagination.Kind == "none" || d.Pagination.Kind == "url" || d.Pagination.Kind == "cursor") {
			a := new(Attempt)
			ok, e := s.ID(c.Attempt.ID).Get(a)
			if e != nil {
				return e
			}
			canRetry = ok && len(unresolvedCommands(a.CommandRefs)) == 0
		}
	}
	if result.Error != nil && canRetry && item.Attempt < 3 {
		status = "queued"
		nextRetry = time.Now().Add(time.Duration(item.Attempt*item.Attempt) * time.Second)
	} else if result.Error != nil && result.Error.Code == "BATCH_COMMIT_FAILED" && !canRetry {
		status = "awaiting_confirmation"
	}
	problem := "{}"
	if result.Error != nil {
		problem = collector.JSON(result.Error)
	}
	if _, e = s.Exec("UPDATE co_work_items SET status=?,error=CAST(? AS jsonb),next_retry_at=?,lease_token=NULL,lease_until=NULL WHERE id=?", status, problem, nextRetry, item.ID); e != nil {
		return e
	}
	if _, e = s.Exec("UPDATE co_work_attempts SET status=?,error=CAST(? AS jsonb),finished_at=NOW() WHERE id=?", status, problem, c.Attempt.ID); e != nil {
		return e
	}
	if e = event(s, r, "item_"+status, item.ID, map[string]any{"attempt": item.Attempt, "reason": result.Reason, "error": result.Error}); e != nil {
		return e
	}
	if (status == "failed" || status == "partial") && c.Manifest.Definition.Stages[0].OnError == "pause_stage" && r.Status != "pausing" {
		r.Status = "pausing"
		r.ControlEpoch++
		if _, e = s.Exec("UPDATE co_runs SET status='pausing',control_epoch=? WHERE id=?", r.ControlEpoch, r.ID); e != nil {
			return e
		}
	}
	if e = settle(s, r); e != nil {
		return e
	}
	return s.Commit()
}
func retryableRead(d collector.Definition, e *collector.Error) bool {
	return d.Fetch.Kind != "browser" && d.Fetch.Method != "POST" && (e.Code == "HTTP_REQUEST_FAILED" || e.Retryable)
}

// Explicit resolution is an operator decision; receipt absence never permits automatic replay.
func ResolveItem(ctx context.Context, owner int64, runID, itemID, key, action string, confirmed bool) (*Operation, error) {
	if !confirmed || (action != "fail" && action != "restart") {
		return nil, collector.Invalid("confirmation", "确认失败或从入口重采；重采可能再次执行网页动作")
	}
	return Mutate(ctx, owner, "resolveItem", runID, key, map[string]any{"item_id": itemID, "action": action, "confirmed": confirmed}, false, func(s *xorm.Session, o *Operation) (any, error) {
		r, e := getRun(s, owner, runID, true)
		if e != nil {
			return nil, e
		}
		item := new(WorkItem)
		ok, e := s.SQL("SELECT * FROM co_work_items WHERE run_id=? AND id=? FOR UPDATE", runID, itemID).Get(item)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		if item.Status != "awaiting_confirmation" || r.Status != "awaiting_confirmation" {
			return nil, conflict("当前工作项无需确认")
		}
		status := "failed"
		if action == "restart" {
			status = "queued"
		}
		if _, e = s.Exec("UPDATE co_work_items SET status=?,checkpoint=CASE WHEN ?='queued' THEN CAST('{}' AS jsonb) ELSE checkpoint END,lease_token=NULL,lease_until=NULL WHERE id=?", status, status, itemID); e != nil {
			return nil, e
		}
		if _, e = s.Exec("UPDATE co_runs SET status='preparing',control_epoch=control_epoch+1 WHERE id=?", runID); e != nil {
			return nil, e
		}
		r.Status = "preparing"
		if e = event(s, r, "operator_"+action, itemID, map[string]bool{"confirmed": true}); e != nil {
			return nil, e
		}
		if e = settle(s, r); e != nil {
			return nil, e
		}
		return map[string]string{"id": itemID, "status": status}, nil
	})
}
func lockRecordKey(s *xorm.Session, owner int64, table, key string) error {
	return idempotency.Lock(s, owner, "co:record:"+table, key)
}
