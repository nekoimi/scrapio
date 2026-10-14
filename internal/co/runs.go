package co

import (
	"context"
	"encoding/json"
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
	BlockedReason      string
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
	StageID  string
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
	if e := idempotency.Lock(s, owner, "co:run-quota", "owner"); e != nil {
		return nil, e
	}
	n, e := s.Where("owner_id=? AND status IN ('preparing','running','pausing','paused','awaiting_confirmation')", owner).Count(new(Run))
	if e != nil {
		return nil, e
	}
	if n >= int64(capacitySetting("SCRAPIO_CO_ACTIVE_RUNS", 20, 1, 100)) {
		return nil, conflict("活动运行达到容量上限，请结束已有运行")
	}
	var m TaskManifest
	if e := collector.Strict([]byte(v.Manifest), &m); e != nil {
		return nil, e
	}
	if len(inputs) == 0 {
		return nil, collector.Invalid("inputs", "没有可执行输入")
	}
	if len(inputs) > m.Definition.Budget.MaxWorkItems {
		return nil, collector.Invalid("budget", "输入数量超过运行工作项预算")
	}
	if e := m.Definition.Validate(); e != nil {
		return nil, e
	}
	for _, st := range m.Definition.Stages {
		if _, e := stageSchemas(s, owner, st, m.ForStage(st.ID).CollectorDefinition); e != nil {
			return nil, e
		}
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
	for _, st := range m.Definition.Stages {
		stage := StageRun{ID: uuid.NewString(), RunID: run.ID, StageID: st.ID, CollectorVersionID: st.CollectorVersionID}
		if _, e := s.Insert(&stage); e != nil {
			return nil, e
		}
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
func StartTask(ctx context.Context, owner int64, taskID, key, versionID, namespace string, options ...TrialOptions) (*Operation, error) {
	return startTask(ctx, owner, taskID, key, versionID, namespace, nil, options...)
}
func startTask(ctx context.Context, owner int64, taskID, key, versionID, namespace string, schedule *Schedule, options ...TrialOptions) (*Operation, error) {
	if namespace != "formal" && namespace != "trial" {
		return nil, collector.Invalid("namespace", "请选择formal/trial")
	}
	var trial *TrialOptions
	if namespace == "trial" {
		trial = &TrialOptions{Mode: "live"}
		if len(options) > 0 {
			trial = &options[0]
		}
		if trial.Mode != "live" && trial.Mode != "offline" {
			return nil, collector.Invalid("trial.mode", "请选择live/offline")
		}
	} else if len(options) > 0 {
		return nil, collector.Invalid("trial", "正式执行不能附加离线样例")
	}
	return Mutate(ctx, owner, "startTask", taskID, key, map[string]any{"task_version_id": versionID, "namespace": namespace, "trial": trial}, false, func(s *xorm.Session, o *Operation) (value any, err error) {
		task, e := getTask(s, owner, taskID, true)
		if e != nil {
			return nil, e
		}
		if task.Archived {
			return nil, conflict("任务已归档，不能触发")
		}
		if schedule != nil {
			current := new(Schedule)
			ok, e := s.SQL("SELECT * FROM co_schedules WHERE id=? FOR UPDATE", schedule.ID).Get(current)
			if e != nil {
				return nil, e
			}
			if !ok || !current.Enabled || current.Revision != schedule.Revision || !current.NextRunAt.Equal(schedule.NextRunAt) {
				return nil, collector.Fault("SCHEDULE_CHANGED", "schedule", "计划已修改，本次旧时刻不再触发")
			}
		}
		defer func() {
			if err != nil {
				return
			}
			data, ok := value.(map[string]any)
			if !ok {
				return
			}
			kind, status, triggerKey := "api", "started", "api:"+taskID+":"+key
			var scheduleID, runID any
			if data["skipped"] == true {
				status = "skipped_overlap"
			} else {
				runID = data["id"]
			}
			if schedule != nil {
				kind = "schedule"
				triggerKey = key
				scheduleID = schedule.ID
			}
			_, err = s.Exec(`INSERT INTO co_triggers(id,owner_id,task_id,task_version_id,schedule_id,trigger_key,kind,status,run_id,data) VALUES(?,?,?,?,?,?,?,?,?,CAST(? AS jsonb))`, uuid.NewString(), owner, taskID, versionID, scheduleID, triggerKey, kind, status, runID, collector.JSON(map[string]any{"operation_id": o.ID, "namespace": namespace, "result": data}))
			if err == nil && schedule != nil {
				_, err = s.Exec(`UPDATE co_schedules SET next_run_at=next_run_at + (FLOOR(EXTRACT(EPOCH FROM (NOW()-next_run_at))/interval_seconds)+1)*interval_seconds*INTERVAL '1 second' WHERE id=?`, schedule.ID)
			}
		}()
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
		if trial != nil {
			if len(m.Stages) == 0 {
				m.Stages = map[string]FrozenStage{m.Definition.EntryStage: {Collector: m.Collector, Definition: m.CollectorDefinition, Schema: m.Schema}}
			}
			if len(collector.JSON(trial)) > 768*1024 {
				return nil, collector.Invalid("trial.documents", "试采样例总量最多768KiB（整个请求最多1MiB）")
			}
			for id, docs := range trial.Documents {
				st := m.Stage(id)
				if st.ID == "" || len(docs) > 20 {
					return nil, collector.Invalid("trial.documents."+id, "阶段不存在或样例超过20页")
				}
				d := m.ForStage(id).CollectorDefinition
				for _, doc := range docs {
					if len(doc.Content) > 512*1024 || (doc.Format != "html" && doc.Format != "json") {
						return nil, collector.Invalid("trial.documents."+id, "样例格式或512KiB页预算无效")
					}
					if e = collector.CheckOrigin(doc.URL, d.Fetch.AllowedOrigins); e != nil {
						return nil, e
					}
				}
			}
			m.Trial = trial
			if m.Definition.Budget.MaxWorkItems > 100 {
				m.Definition.Budget.MaxWorkItems = 100
			}
			if m.Definition.Budget.MaxOutputs > 500 {
				m.Definition.Budget.MaxOutputs = 500
			}
			if m.Definition.Budget.MaxDurationMS > 120000 {
				m.Definition.Budget.MaxDurationMS = 120000
			}
			for id, f := range m.Stages {
				if f.Definition.Pagination.MaxPages > 3 {
					f.Definition.Pagination.MaxPages = 3
				}
				if f.Definition.Pagination.MaxRecords > 100 {
					f.Definition.Pagination.MaxRecords = 100
				}
				if f.Definition.Pagination.MaxDurationMS > 60000 {
					f.Definition.Pagination.MaxDurationMS = 60000
				}
				m.Stages[id] = f
			}
			m = m.ForStage(m.Definition.EntryStage)
			m.Hash = ""
			m.Hash = collector.Hash(m)
			v.Manifest = collector.JSON(m)
		}
		inputs, e := frozenSourceInputs(s, owner, m.Definition.Source, m.CollectorDefinition)
		if e != nil {
			return nil, e
		}
		result, e := newRun(s, owner, *v, namespace, "", inputs, []string{})
		if e != nil {
			return nil, e
		}
		return result, nil
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
		var originalManifest TaskManifest
		if e = collector.Strict([]byte(original.Manifest), &originalManifest); e != nil {
			return nil, e
		}
		if len(originalManifest.Definition.Stages) > 1 {
			return nil, collector.Fault("SCOPED_RETRY_REQUIRED", "retry", "多阶段局部补采在O4开放；请使用明确的新运行，不能把中间输入当入口重放")
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
		v.Manifest = original.Manifest // Scoped runs may use a pruned entry and frozen trial documents.
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
func settle(s *xorm.Session, r *Run) error { return graphSettle(s, r) }

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
			if _, e = s.Exec("UPDATE co_stage_runs SET blocked_reason='' WHERE run_id=?", id); e != nil {
				return nil, e
			}
		case "cancel":
			r.Status = "cancelled"
			r.ControlEpoch++
			if _, e = s.Exec("UPDATE co_dispatch_plans SET status='cancelled' WHERE run_id=? AND status='pending'", id); e != nil {
				return nil, e
			}
			if _, e = s.Exec("UPDATE co_work_items SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE run_id=? AND status IN ('queued','running','backpressured','awaiting_confirmation')", id); e != nil {
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
	ids, e := s.QueryString("SELECT id FROM co_runs WHERE status IN ('preparing','running') ORDER BY last_maintenance_at NULLS FIRST,created_at LIMIT 20")
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
	if _, e = s.Exec("UPDATE co_runs SET last_maintenance_at=NOW() WHERE id=?", id); e != nil {
		return e
	}
	var m TaskManifest
	if e = collector.Strict([]byte(r.Manifest), &m); e != nil {
		return e
	}
	if time.Since(r.CreatedAt) > time.Duration(m.Definition.Budget.MaxDurationMS)*time.Millisecond {
		if _, e = s.Exec("UPDATE co_work_items SET status='limited',lease_token=NULL,lease_until=NULL WHERE run_id=? AND status IN ('queued','running','backpressured')", id); e != nil {
			return e
		}
		r.Status = "limited"
		if _, e = s.Exec("UPDATE co_runs SET status='limited',finished_at=NOW() WHERE id=?", id); e != nil {
			return e
		}
		if _, e = s.Exec("UPDATE co_work_attempts SET status='limited',finished_at=NOW() WHERE run_id=? AND status='running'", id); e != nil {
			return e
		}
		if _, e = s.Exec("UPDATE co_dispatch_plans SET status='limited' WHERE run_id=? AND status='pending'", id); e != nil {
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
	ok, e := s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? AND stage_id=? FOR UPDATE", id, m.Definition.EntryStage).Get(stage)
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
	// Give committed downstream plans priority over new root inputs. Otherwise a
	// one-slot queue can be filled by a producer that is fenced by its own backlog.
	backlog, e := s.Table("co_dispatch_plans").Where("run_id=? AND status='pending'", id).Count()
	if e != nil {
		return e
	}
	if backlog > 0 {
		capacity = 0
	}
	if capacity > 10 {
		capacity = 10
	}
	if stage.BlockedReason == "" {
		inputs := []RunInput{}
		if e = s.Where("run_id=? AND ordinal>=? AND ordinal<?", id, r.PreparingCursor, r.InputCount).OrderBy("ordinal").Limit(10).Find(&inputs); e != nil {
			return e
		}
		prepared := 0
		for _, in := range inputs {
			st := m.Stage(stage.StageID)
			itemID, err := mergedItem(s, id, stage.ID, st, in.Values)
			if err != nil {
				return err
			}
			merged := itemID != ""
			if !merged {
				if capacity <= 0 {
					break
				}
				itemID, e = createInputItem(s, id, stage.ID, st, m.ForStage(stage.StageID).CollectorDefinition, in)
				if e != nil {
					return e
				}
				capacity--
			}
			if e = attachInputSource(s, id, itemID, in.ID, merged); e != nil {
				return e
			}
			if _, e = s.Exec("UPDATE co_run_inputs SET materialized_at=COALESCE(materialized_at,NOW()) WHERE id=?", in.ID); e != nil {
				return e
			}
			r.PreparingCursor = in.Ordinal + 1
			prepared++
		}
		if _, e = s.Exec("UPDATE co_runs SET preparing_cursor=? WHERE id=?", r.PreparingCursor, id); e != nil {
			return e
		}
		if prepared > 0 {
			if e = event(s, r, "inputs_materialized", "", map[string]int{"prepared": r.PreparingCursor, "total": r.InputCount}); e != nil {
				return e
			}
		}
	}
	if r.PreparingCursor == r.InputCount {
		if _, e = s.Exec("UPDATE co_stage_runs SET input_closed=TRUE,close_reason='frozen_root_snapshot_complete' WHERE id=?", stage.ID); e != nil {
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
	kindSQL := "CASE WHEN r.manifest -> 'trial' ->> 'mode'='offline' THEN 'http_html' ELSE COALESCE(r.manifest -> 'stage_manifests' -> st.stage_id -> 'definition' -> 'fetch' ->> 'kind',r.manifest -> 'collector_definition' -> 'fetch' ->> 'kind') END"
	eligible := "((r.status IN ('preparing','running') AND w.status='queued' AND st.blocked_reason='' AND NOT EXISTS(SELECT 1 FROM co_dispatch_plans backlog WHERE backlog.run_id=r.id AND backlog.from_stage=st.stage_id AND backlog.status='pending') AND (w.next_retry_at IS NULL OR w.next_retry_at<=NOW())) OR (w.status='running' AND w.lease_until<NOW()))"
	slotSQL := " AND (w.status='running' OR ?=FALSE OR (SELECT COUNT(*) FROM co_work_items active JOIN co_stage_runs ast ON ast.id=active.stage_run_id WHERE active.run_id=r.id AND active.status='running' AND active.lease_until>NOW() AND COALESCE(r.manifest -> 'stage_manifests' -> ast.stage_id -> 'definition' -> 'fetch' ->> 'kind',r.manifest -> 'collector_definition' -> 'fetch' ->> 'kind')='browser')<CAST(r.manifest -> 'definition' -> 'budget' ->> 'browser_slots' AS INT))"
	ok, e := s.SQL("SELECT r.* FROM co_runs r WHERE r.status IN ('preparing','running','pausing','awaiting_confirmation') AND EXISTS(SELECT 1 FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=r.id AND (("+kindSQL+"='browser')=?) AND "+eligible+slotSQL+") ORDER BY r.last_claim_at NULLS FIRST,r.created_at FOR UPDATE OF r SKIP LOCKED LIMIT 1", browser, browser).Get(r)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, s.Commit()
	}
	stage := new(StageRun)
	ok, e = s.SQL("SELECT st.* FROM co_stage_runs st JOIN co_runs r ON r.id=st.run_id WHERE st.run_id=? AND EXISTS(SELECT 1 FROM co_work_items w WHERE w.stage_run_id=st.id AND (("+kindSQL+"='browser')=?) AND "+eligible+") ORDER BY st.last_claim_at NULLS FIRST,st.stage_id FOR UPDATE OF st SKIP LOCKED LIMIT 1", r.ID, browser).Get(stage)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, s.Commit()
	}
	item := new(WorkItem)
	ok, e = s.SQL(`SELECT w.* FROM co_work_items w WHERE w.stage_run_id=? AND ((? IN ('preparing','running') AND w.status='queued' AND (w.next_retry_at IS NULL OR w.next_retry_at<=NOW())) OR (w.status='running' AND w.lease_until<NOW())) ORDER BY (SELECT COUNT(*) FROM co_work_items active WHERE active.run_id=w.run_id AND active.site_key=w.site_key AND active.status='running'),w.created_at,w.id FOR UPDATE OF w SKIP LOCKED LIMIT 1`, stage.ID, r.Status).Get(item)
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
	m = m.ForStage(stage.StageID)
	if _, e = s.Exec("UPDATE co_runs SET last_claim_at=NOW() WHERE id=?", r.ID); e != nil {
		return nil, e
	}
	if item.Status == "queued" {
		var proof map[string]any
		if item.Attempt == 0 {
			proof, e = skipFresh(s, r, m, stage.StageID, item.InputHash)
		}
		if e != nil {
			return nil, e
		}
		if proof != nil {
			if _, e = s.Exec("UPDATE co_work_items SET status='skipped',error=CAST(? AS jsonb) WHERE id=?", collector.JSON(proof), item.ID); e != nil {
				return nil, e
			}
			if e = event(s, r, "input_refresh_skipped", item.ID, proof); e != nil {
				return nil, e
			}
			if e = settle(s, r); e != nil {
				return nil, e
			}
			return nil, s.Commit()
		}
		site := executionSiteInput(m.CollectorDefinition, item.InputValues)
		if site != "" && capacitySetting("SCRAPIO_CO_SITE_INTERVAL_MS", 500, 0, 60000) > 0 {
			if _, e = s.Exec("INSERT INTO co_site_limits(site_key) VALUES(?) ON CONFLICT DO NOTHING", site); e != nil {
				return nil, e
			}
			limit := struct{ NextStartAt time.Time }{}
			if _, e = s.SQL("SELECT next_start_at FROM co_site_limits WHERE site_key=? FOR UPDATE", site).Get(&limit); e != nil {
				return nil, e
			}
			if limit.NextStartAt.After(time.Now()) {
				if _, e = s.Exec("UPDATE co_work_items SET next_retry_at=? WHERE id=?", limit.NextStartAt, item.ID); e != nil {
					return nil, e
				}
				return nil, s.Commit()
			}
			if _, e = s.Exec("UPDATE co_site_limits SET next_start_at=NOW()+?*INTERVAL '1 millisecond' WHERE site_key=?", capacitySetting("SCRAPIO_CO_SITE_INTERVAL_MS", 500, 0, 60000), site); e != nil {
				return nil, e
			}
		}
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
		if status == "succeeded" && r.Namespace == "formal" {
			if e = recordSuccessfulInput(s, r, m, stage.StageID, item); e != nil {
				return nil, e
			}
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
				return &WorkClaim{Run: *r, Item: *item, Attempt: a, Manifest: m, StageID: stage.StageID, Recovery: true}, nil
			}
		}
		return nil, nil
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
	if _, e = s.Exec("UPDATE co_stage_runs SET last_claim_at=NOW() WHERE id=?", stage.ID); e != nil {
		return nil, e
	}
	if e = event(s, r, "item_started", item.ID, map[string]any{"attempt": item.Attempt}); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return &WorkClaim{Run: *r, Item: *item, Attempt: attempt, Manifest: m, StageID: stage.StageID}, nil
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
	if result.Error == nil && status == "succeeded" && !checkpoint.Done {
		status = "failed"
		result.Error = collector.AsError(collector.Fault("FINAL_BATCH_NOT_COMMITTED", "checkpoint", "最后一批尚未提交，不能声明工作项完成"))
	}
	var invalidCount int64
	invalidCount, e = s.Where("item_id=? AND decision IN ('invalid','conflict')", item.ID).Table("co_observations").Count()
	if e != nil {
		return e
	}
	if invalidCount > 0 && status == "succeeded" {
		status = "partial"
	}
	if result.Reason == "OUTPUT_BUDGET" || (result.Error != nil && (result.Error.Code == "DISPATCH_BUDGET" || result.Error.Code == "OUTPUT_BUDGET" || result.Error.Code == "OUTPUT_STORAGE_LIMIT" || result.Error.Code == "DISPATCH_PAYLOAD_BUDGET")) {
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
	if result.Error != nil && result.Error.Code == "BACKPRESSURE_UNSAFE" {
		status = "awaiting_confirmation"
		nextRetry = nil
	}
	if result.Error != nil && result.Error.Code == "BACKPRESSURE" {
		status = "backpressured"
		nextRetry = nil
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
	if status == "succeeded" && r.Namespace == "formal" {
		if e = recordSuccessfulInput(s, r, c.Manifest, c.StageID, item); e != nil {
			return e
		}
	}
	if e = event(s, r, "item_"+status, item.ID, map[string]any{"attempt": item.Attempt, "reason": result.Reason, "error": result.Error}); e != nil {
		return e
	}
	shared := result.Error != nil && (result.Error.Code == "CREDENTIAL_UNAVAILABLE" || result.Error.Code == "BROWSER_UNAVAILABLE" || result.Error.Code == "CAPABILITY_UNAVAILABLE")
	if ((status == "failed" || status == "partial") && c.Manifest.Stage(c.StageID).OnError == "pause_stage") || shared {
		if _, e = s.Exec("UPDATE co_stage_runs SET blocked_reason=? WHERE id=?", problem, item.StageRunID); e != nil {
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
