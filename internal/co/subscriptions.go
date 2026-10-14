package co

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

// A stream counter is assigned under a per-table transaction lock, not a
// sequence/updated_at cursor: later commits cannot overtake an unseen event.
type TableStream struct {
	TableID       string `xorm:"table_id pk"`
	Sequence      int64
	RetainedAfter int64
}

func (TableStream) TableName() string { return "co_table_streams" }

type RecordChange struct {
	TableID        string `xorm:"table_id"`
	Sequence       int64
	OwnerID        int64  `xorm:"owner_id"`
	RecordID       string `xorm:"record_id"`
	Revision       int
	SchemaVersion  int
	Kind           string
	Values         string `xorm:"jsonb values_json"`
	ObservationID  string `xorm:"observation_id"`
	AccountedBytes int64
	CreatedAt      time.Time
}

func (RecordChange) TableName() string { return "co_record_changes" }

type SubscriptionRequest struct {
	TaskVersionID string            `json:"task_version_id"`
	TableID       string            `json:"table_id"`
	SchemaVersion int               `json:"schema_version"`
	SchemaHash    string            `json:"schema_hash"`
	Mapping       map[string]string `json:"mapping"`
	InitialMode   string            `json:"initial_mode"`
	BatchSize     int               `json:"batch_size"`
	Enabled       bool              `json:"enabled"`
	Confirmed     bool              `json:"confirmed"`
}
type SubscriptionControl struct {
	ID               string `json:"id"`
	ExpectedRevision int    `json:"expected_revision"`
	Action           string `json:"action"`
}
type Subscription struct {
	ID             string `xorm:"id pk"`
	OwnerID        int64  `xorm:"owner_id"`
	TaskID         string `xorm:"task_id"`
	TaskVersionID  string `xorm:"task_version_id"`
	TableID        string `xorm:"table_id"`
	SchemaVersion  int
	SchemaHash     string
	Mapping        string `xorm:"jsonb mapping"`
	WriteTables    string `xorm:"jsonb write_tables"`
	InitialMode    string
	BatchSize      int
	Enabled        bool
	Revision       int
	Cursor         int64
	Backfill       string `xorm:"jsonb backfill"`
	BackfillCursor int
	AccountedBytes int64
	Status         string
	Error          string `xorm:"jsonb error"`
	CreatedAt      time.Time
}

func (Subscription) TableName() string { return "co_subscriptions" }

func lockTableStream(s *xorm.Session, table string) (*TableStream, error) {
	if _, e := s.Exec("INSERT INTO co_table_streams(table_id) VALUES(?) ON CONFLICT DO NOTHING", table); e != nil {
		return nil, e
	}
	stream := new(TableStream)
	_, e := s.SQL("SELECT * FROM co_table_streams WHERE table_id=? FOR UPDATE", table).Get(stream)
	return stream, e
}
func lockSaveStreams(s *xorm.Session, stage TaskStage) error {
	tables := []string{}
	for _, p := range stage.Saves() {
		if !passThrough(p.Save) {
			tables = append(tables, p.Save.TableID)
		}
	}
	sort.Strings(tables)
	for _, table := range tables {
		if _, e := lockTableStream(s, table); e != nil {
			return e
		}
	}
	return nil
}
func appendRecordChange(s *xorm.Session, r *Run, record *BusinessRecord, observation, kind string) error {
	// Called after record+observation writes, under lockSaveStreams, in CommitBatch.
	bytes := int64(len(record.Values) + 256)
	if e := reserveStorage(s, r.OwnerID, "change_bytes", bytes); e != nil {
		return e
	}
	if _, e := s.Exec("UPDATE co_table_streams SET sequence=sequence+1 WHERE table_id=?", record.TableID); e != nil {
		return e
	}
	_, e := s.Exec(`INSERT INTO co_record_changes(table_id,sequence,owner_id,record_id,revision,schema_version,kind,values_json,observation_id,accounted_bytes) SELECT ?,sequence,?,?,?,?,?,CAST(? AS jsonb),?,? FROM co_table_streams WHERE table_id=?`, record.TableID, r.OwnerID, record.ID, record.Revision, record.SchemaVersion, kind, record.Values, observation, bytes, record.TableID)
	return e
}
func subscriptionWrites(m TaskManifest) []string {
	out := []string{}
	for _, st := range m.Definition.Stages {
		for _, p := range st.Saves() {
			if !passThrough(p.Save) {
				out = append(out, p.Save.TableID)
			}
		}
	}
	sort.Strings(out)
	return out
}
func subscriptionCycle(edges map[string][]string, source string, writes []string) bool {
	var reaches func(string, map[string]bool) bool
	reaches = func(node string, seen map[string]bool) bool {
		if node == source {
			return true
		}
		if seen[node] {
			return false
		}
		seen[node] = true
		for _, next := range edges[node] {
			if reaches(next, seen) {
				return true
			}
		}
		return false
	}
	for _, target := range writes {
		if reaches(target, map[string]bool{}) {
			return true
		}
	}
	return false
}
func subscriptionMapping(schema TableSchema, d collector.Definition, mapping map[string]string) error {
	if len(mapping) == 0 || len(mapping) > 30 {
		return collector.Invalid("mapping", "需要1..30个来源列到入口输入的映射")
	}
	fields := map[string]collector.Field{}
	for _, f := range schema.Fields {
		fields[f.Key] = f
	}
	targets := map[string]collector.Field{}
	for _, f := range d.InputFields {
		targets[f.Key] = f
	}
	for target, column := range mapping {
		dst, ok := targets[target]
		src, exists := fields[column]
		if !ok || !exists {
			return collector.Invalid("mapping."+target, "输入字段或来源表列不存在")
		}
		if dst.Multiple != src.Multiple || !(dst.Type == src.Type || (src.Type == "integer" && dst.Type == "number")) {
			return collector.Invalid("mapping."+target, "来源与输入类型不兼容；URL输入需映射URL列")
		}
	}
	for _, f := range d.InputFields {
		if f.Required && len(f.Default) == 0 && mapping[f.Key] == "" {
			return collector.Invalid("mapping."+f.Key, "必填入口输入缺少映射或默认值")
		}
	}
	return nil
}
func subscriptionInput(d collector.Definition, mapping map[string]string, raw string) (map[string]any, error) {
	values := map[string]any{}
	if e := collector.Strict([]byte(raw), &values); e != nil {
		return nil, e
	}
	input := map[string]any{}
	for target, column := range mapping {
		if v, ok := values[column]; ok {
			input[target] = v
		}
	}
	v, e := collector.ValidateInput(d, input)
	if e == nil {
		_, e = collector.Prepare(d, v)
	}
	return v, e
}

func CreateSubscription(ctx context.Context, owner int64, task, key string, p SubscriptionRequest) (*Operation, error) {
	if !p.Confirmed || (p.InitialMode != "backfill" && p.InitialMode != "future") || p.BatchSize < 1 || p.BatchSize > 100 {
		return nil, collector.Invalid("subscription", "请确认回补/仅未来及1..100条批次")
	}
	return Mutate(ctx, owner, "createSubscription", task, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		t, e := getTask(s, owner, task, true)
		if e != nil {
			return nil, e
		}
		if t.Archived {
			return nil, conflict("任务已归档")
		}
		// Serialize graph edits, including paused definitions; enable cannot reveal a cycle.
		if _, e = s.Exec("SELECT pg_advisory_xact_lock(hashtextextended('co-subscription-graph',0))"); e != nil {
			return nil, e
		}
		existing, e := s.Where("task_id=? AND status<>'retired'", task).Count(new(Subscription))
		if e != nil {
			return nil, e
		}
		if existing > 0 {
			return nil, conflict("任务已有订阅；暂停保留游标，结束后可显式创建新订阅")
		}
		n, e := s.Where("owner_id=? AND status<>'retired'", owner).Count(new(Subscription))
		if e != nil {
			return nil, e
		}
		if n >= 20 {
			return nil, conflict("最多20个保留中的订阅，请结束不再需要的订阅")
		}
		v := new(TaskVersion)
		ok, e := s.Where("owner_id=? AND task_id=? AND id=?", owner, task, p.TaskVersionID).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		var m TaskManifest
		if e = collector.Strict([]byte(v.Manifest), &m); e != nil {
			return nil, e
		}
		schema, e := tableSchema(s, owner, SaveBinding{TableID: p.TableID, SchemaVersion: p.SchemaVersion, SchemaHash: p.SchemaHash}, true)
		if e != nil {
			return nil, e
		}
		d := m.ForStage(m.Definition.EntryStage).CollectorDefinition
		if p.BatchSize > m.Definition.Budget.MaxWorkItems {
			return nil, collector.Invalid("batch_size", "批次超过任务工作项预算")
		}
		if e = subscriptionMapping(schema, d, p.Mapping); e != nil {
			return nil, e
		}
		others := []Subscription{}
		if e = s.Where("owner_id=? AND status<>'retired'", owner).Find(&others); e != nil {
			return nil, e
		}
		edges := map[string][]string{}
		for _, sub := range others {
			var writes []string
			if e = json.Unmarshal([]byte(sub.WriteTables), &writes); e != nil {
				return nil, e
			}
			edges[sub.TableID] = append(edges[sub.TableID], writes...)
		}
		writes := subscriptionWrites(m)
		if subscriptionCycle(edges, p.TableID, writes) {
			return nil, collector.Fault("SUBSCRIPTION_CYCLE", "table_id", "订阅及其保存表构成环路；请使用不回写上游的独立输出表")
		}
		stream, e := lockTableStream(s, p.TableID)
		if e != nil {
			return nil, e
		}
		backfill := []RunInput{}
		if p.InitialMode == "backfill" {
			records := []BusinessRecord{}
			if e = s.Where("table_id=?", p.TableID).OrderBy("id").Limit(1001).Find(&records); e != nil {
				return nil, e
			}
			if len(records) > 1000 {
				return nil, collector.Invalid("initial_mode", "初始回补最多1000条，不能静默截断；请先做有限表快照任务或仅订阅未来")
			}
			for _, record := range records {
				input, e := subscriptionInput(d, p.Mapping, record.Values)
				if e != nil {
					return nil, collector.Invalid("backfill."+record.ID, e.Error())
				}
				backfill = append(backfill, RunInput{SourceKey: fmt.Sprintf("backfill:%s:%d", record.ID, record.Revision), Values: collector.JSON(input), Source: collector.JSON(map[string]any{"kind": "subscription_backfill", "table_id": p.TableID, "record_id": record.ID, "revision": record.Revision, "schema_version": record.SchemaVersion, "values": json.RawMessage(record.Values), "watermark": stream.Sequence})})
			}
		}
		encoded := collector.JSON(backfill)
		if len(encoded) > 4*1024*1024 {
			return nil, collector.Invalid("backfill", "回补快照超过4MiB，请使用有限快照或仅订阅未来")
		}
		sub := Subscription{ID: uuid.NewString(), OwnerID: owner, TaskID: task, TaskVersionID: v.ID, TableID: p.TableID, SchemaVersion: p.SchemaVersion, SchemaHash: p.SchemaHash, Mapping: collector.JSON(p.Mapping), WriteTables: collector.JSON(writes), InitialMode: p.InitialMode, BatchSize: p.BatchSize, Enabled: p.Enabled, Revision: 1, Cursor: stream.Sequence, Backfill: encoded, AccountedBytes: int64(len(encoded)), Status: "ready", Error: "{}", CreatedAt: time.Now()}
		if e = reserveStorage(s, owner, "source_bytes", sub.AccountedBytes); e != nil {
			return nil, e
		}
		if _, e = s.Insert(&sub); e != nil {
			return nil, e
		}
		return map[string]any{"id": sub.ID, "revision": 1, "cursor": sub.Cursor, "backfill_count": len(backfill), "enabled": sub.Enabled, "task_version_id": v.ID}, nil
	})
}
func releaseSubscriptionBackfill(s *xorm.Session, sub *Subscription) error {
	if _, e := s.Exec("UPDATE co_storage_usage SET source_bytes=GREATEST(0,source_bytes-?) WHERE owner_id=?", sub.AccountedBytes, sub.OwnerID); e != nil {
		return e
	}
	sub.AccountedBytes = 0
	sub.Backfill = "[]"
	sub.BackfillCursor = 0
	_, e := s.Exec("UPDATE co_subscriptions SET backfill='[]',backfill_cursor=0,accounted_bytes=0 WHERE id=?", sub.ID)
	return e
}
func ControlSubscription(ctx context.Context, owner int64, task, key string, p SubscriptionControl) (*Operation, error) {
	if p.Action != "pause" && p.Action != "resume" && p.Action != "retire" {
		return nil, collector.Invalid("action", "请选择pause/resume/retire")
	}
	return Mutate(ctx, owner, "controlSubscription", task, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		t, e := getTask(s, owner, task, true)
		if e != nil {
			return nil, e
		}
		if _, e = s.Exec("SELECT pg_advisory_xact_lock(hashtextextended('co-subscription-graph',0))"); e != nil {
			return nil, e
		}
		sub := new(Subscription)
		ok, e := s.SQL("SELECT * FROM co_subscriptions WHERE id=? AND task_id=? AND owner_id=? FOR UPDATE", p.ID, task, owner).Get(sub)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		if sub.Revision != p.ExpectedRevision || sub.Status == "retired" {
			return nil, conflict("订阅已变化或已结束，请重新加载")
		}
		if p.Action == "resume" && (t.Archived || sub.Status != "ready") {
			return nil, conflict("任务已归档或订阅被阻断；请查看错误，修正规则后结束并创建新订阅")
		}
		sub.Enabled = p.Action == "resume"
		sub.Revision++
		if p.Action == "retire" {
			sub.Status = "retired"
			if e = releaseSubscriptionBackfill(s, sub); e != nil {
				return nil, e
			}
		}
		_, e = s.Exec("UPDATE co_subscriptions SET enabled=?,revision=?,status=? WHERE id=?", sub.Enabled, sub.Revision, sub.Status, sub.ID)
		return map[string]any{"id": sub.ID, "revision": sub.Revision, "enabled": sub.Enabled, "status": sub.Status, "cursor": sub.Cursor, "active_runs": "unchanged"}, e
	})
}
func TaskSubscriptions(ctx context.Context, owner int64, task string) (any, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getTask(s, owner, task, false); e != nil {
		return nil, e
	}
	rows, e := views(s, `SELECT x.id,x.task_version_id,x.table_id,x.schema_version,x.schema_hash,x.mapping,x.initial_mode,x.batch_size,x.enabled,x.revision,x.cursor,x.backfill_cursor,jsonb_array_length(x.backfill) AS backfill_count,x.status,x.error,x.created_at,t.name AS table_name,COALESCE(st.sequence,0) AS latest_sequence,COALESCE(st.sequence,0)-x.cursor AS pending_changes FROM co_subscriptions x JOIN co_data_tables t ON t.id=x.table_id LEFT JOIN co_table_streams st ON st.table_id=x.table_id WHERE x.owner_id=? AND x.task_id=? ORDER BY x.created_at DESC,x.id DESC LIMIT 20`, owner, task)
	batches, e2 := views(s, `SELECT b.*,r.status AS run_status FROM co_subscription_batches b JOIN co_subscriptions x ON x.id=b.subscription_id JOIN co_runs r ON r.id=b.run_id WHERE x.owner_id=? AND x.task_id=? ORDER BY b.created_at DESC,b.id DESC LIMIT 20`, owner, task)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows, "batches": batches, "batch_limit": 20}, e2
}

func blockSubscription(s *xorm.Session, sub *Subscription, problem error) error {
	_, e := s.Exec("UPDATE co_subscriptions SET status='blocked',enabled=FALSE,revision=revision+1,error=CAST(? AS jsonb) WHERE id=?", collector.JSON(collector.AsError(problem)), sub.ID)
	return e
}
func tickSubscription(ctx context.Context, id string) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	// Task then subscription agrees with control/archive and manual/schedule triggers.
	hint := new(Subscription)
	ok, e := s.ID(id).Get(hint)
	if e != nil {
		return e
	}
	if !ok {
		return nil
	}
	t, e := getTask(s, hint.OwnerID, hint.TaskID, true)
	if e != nil {
		return e
	}
	sub := new(Subscription)
	ok, e = s.SQL("SELECT * FROM co_subscriptions WHERE id=? FOR UPDATE", id).Get(sub)
	if e != nil {
		return e
	}
	if !ok || !sub.Enabled || sub.Status != "ready" {
		return nil
	}
	if _, e = s.Exec("UPDATE co_subscriptions SET last_checked_at=NOW() WHERE id=?", id); e != nil {
		return e
	}
	block := func(problem error) error {
		if e := blockSubscription(s, sub, problem); e != nil {
			return e
		}
		return s.Commit()
	}
	if t.Archived {
		return block(conflict("任务已归档"))
	}
	active, e := s.SQL(`SELECT COUNT(*) FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND r.namespace='formal' AND r.status IN ('preparing','running','pausing','paused','awaiting_confirmation')`, sub.TaskID).Count(new(Run))
	if e != nil {
		return e
	}
	if active > 0 {
		return s.Commit()
	}
	v := new(TaskVersion)
	ok, e = s.Where("owner_id=? AND id=?", sub.OwnerID, sub.TaskVersionID).Get(v)
	if e != nil {
		return e
	}
	if !ok {
		return block(ErrNotFound)
	}
	var m TaskManifest
	if e = collector.Strict([]byte(v.Manifest), &m); e != nil {
		return block(e)
	}
	if _, e = tableSchema(s, sub.OwnerID, SaveBinding{TableID: sub.TableID, SchemaVersion: sub.SchemaVersion, SchemaHash: sub.SchemaHash}, false); e != nil {
		return block(e)
	}
	var mapping map[string]string
	if e = json.Unmarshal([]byte(sub.Mapping), &mapping); e != nil {
		return block(e)
	}
	var backfill []RunInput
	if e = json.Unmarshal([]byte(sub.Backfill), &backfill); e != nil {
		return block(e)
	}
	inputs := []RunInput{}
	kind := "changes"
	first, last := sub.Cursor, sub.Cursor
	nextBackfill := sub.BackfillCursor
	if sub.BackfillCursor < len(backfill) {
		kind = "backfill"
		end := sub.BackfillCursor + sub.BatchSize
		if end > len(backfill) {
			end = len(backfill)
		}
		inputs = append(inputs, backfill[sub.BackfillCursor:end]...)
		first = int64(sub.BackfillCursor)
		last = int64(end)
		nextBackfill = end
	} else {
		stream := new(TableStream)
		ok, e = s.Where("table_id=?", sub.TableID).Get(stream)
		if e != nil {
			return e
		}
		if !ok || sub.Cursor < stream.RetainedAfter {
			return block(collector.Fault("SUBSCRIPTION_CURSOR_EXPIRED", "cursor", "来源变更证据缺失，不能改读当前整表；请结束并明确选择回补或仅未来"))
		}
		changes := []RecordChange{}
		if e = s.Where("table_id=? AND sequence>?", sub.TableID, sub.Cursor).OrderBy("sequence").Limit(sub.BatchSize).Find(&changes); e != nil {
			return e
		}
		if len(changes) == 0 {
			if sub.Cursor < stream.Sequence {
				return block(collector.Fault("SUBSCRIPTION_CURSOR_GAP", "cursor", "变更水位领先但证据缺失；保留游标并停止消费"))
			}
			return s.Commit()
		}
		for _, change := range changes {
			if change.Sequence != last+1 {
				return block(collector.Fault("SUBSCRIPTION_CURSOR_GAP", "cursor", "变更流序号缺失，保留游标且停止消费"))
			}
			if change.SchemaVersion != sub.SchemaVersion {
				return block(collector.Fault("SCHEMA_CONFLICT", "source", "变更Schema与冻结订阅不一致，请重新预演"))
			}
			input, e := subscriptionInput(m.ForStage(m.Definition.EntryStage).CollectorDefinition, mapping, change.Values)
			if e != nil {
				return block(collector.Fault("SUBSCRIPTION_INPUT_INVALID", fmt.Sprintf("event.%d", change.Sequence), e.Error()))
			}
			if len(inputs) == 0 {
				first = change.Sequence
			}
			last = change.Sequence
			inputs = append(inputs, RunInput{SourceKey: fmt.Sprintf("change:%s:%d", sub.TableID, change.Sequence), Values: collector.JSON(input), Source: collector.JSON(map[string]any{"kind": "subscription_change", "table_id": sub.TableID, "sequence": change.Sequence, "record_id": change.RecordID, "revision": change.Revision, "schema_version": change.SchemaVersion, "change_kind": change.Kind, "observation_id": change.ObservationID, "values": json.RawMessage(change.Values)})})
		}
	}
	// Frozen run owns full source provenance; cursor advances only with that run.
	values := []map[string]any{}
	for i := range inputs {
		var value map[string]any
		if e = json.Unmarshal([]byte(inputs[i].Values), &value); e != nil {
			return block(e)
		}
		values = append(values, value)
		var source map[string]any
		if e = json.Unmarshal([]byte(inputs[i].Source), &source); e != nil {
			return block(e)
		}
		source["subscription_id"] = sub.ID
		inputs[i].Source = collector.JSON(source)
	}
	// Published source remains unchanged. This run explicitly freezes this batch.
	m.Definition.Source = ManualSource{Kind: "manual", Inputs: values}
	m.Hash = ""
	m.Hash = collector.Hash(m)
	v.Manifest = collector.JSON(m)
	if e = m.Definition.Validate(); e != nil {
		return block(e)
	}
	if _, e = s.Exec("SAVEPOINT subscription_run"); e != nil {
		return e
	}
	result, e := newRun(s, sub.OwnerID, *v, "formal", "", inputs, []string{})
	if e != nil {
		if _, rollbackErr := s.Exec("ROLLBACK TO SAVEPOINT subscription_run"); rollbackErr != nil {
			return rollbackErr
		}
		var typed *collector.Error
		if errors.As(e, &typed) && typed.Code != "CONFLICT" {
			return block(e)
		}
		if errors.As(e, &typed) && typed.Code == "CONFLICT" {
			return s.Commit()
		}
		return e
	}
	data := result.(map[string]any)
	batchKey := fmt.Sprintf("%s:%d:%d", kind, first, last)
	if _, e = s.Exec(`INSERT INTO co_subscription_batches(id,subscription_id,batch_key,kind,first_sequence,last_sequence,input_count,run_id) VALUES(?,?,?,?,?,?,?,?)`, uuid.NewString(), sub.ID, batchKey, kind, first, last, len(inputs), data["id"]); e != nil {
		return e
	}
	if kind == "backfill" {
		if nextBackfill == len(backfill) {
			if e = releaseSubscriptionBackfill(s, sub); e != nil {
				return e
			}
		} else {
			if _, e = s.Exec("UPDATE co_subscriptions SET backfill_cursor=? WHERE id=?", nextBackfill, sub.ID); e != nil {
				return e
			}
		}
	} else {
		if _, e = s.Exec("UPDATE co_subscriptions SET cursor=? WHERE id=?", last, sub.ID); e != nil {
			return e
		}
	}
	if _, e = s.Exec(`INSERT INTO co_triggers(id,owner_id,task_id,task_version_id,trigger_key,kind,status,run_id,data) VALUES(?,?,?,?,?,'subscription','started',?,CAST(? AS jsonb))`, uuid.NewString(), sub.OwnerID, sub.TaskID, sub.TaskVersionID, "subscription:"+sub.ID+":"+batchKey, data["id"], collector.JSON(map[string]any{"subscription_id": sub.ID, "kind": kind, "first": first, "last": last, "input_count": len(inputs)})); e != nil {
		return e
	}
	return s.Commit()
}
func TickSubscriptions(ctx context.Context) error {
	s := session(ctx)
	due := []Subscription{}
	e := s.Where("enabled=TRUE AND status='ready'").OrderBy("last_checked_at,id").Limit(20).Find(&due)
	s.Close()
	if e != nil {
		return e
	}
	var problems []error
	for _, sub := range due {
		if e = tickSubscription(ctx, sub.ID); e != nil {
			problems = append(problems, e)
		}
	}
	return errors.Join(problems...)
}

// Delete only an old contiguous prefix which every retained subscription has
// handed to durable runs. Paused/blocked subscribers pin their cursor too.
func CleanupChanges(ctx context.Context) error {
	s := session(ctx)
	tables := []DataTable{}
	e := s.SQL(`SELECT t.* FROM co_data_tables t JOIN co_table_streams st ON st.table_id=t.id WHERE EXISTS(SELECT 1 FROM co_record_changes c WHERE c.table_id=t.id AND c.created_at<NOW()-?*INTERVAL '1 day') ORDER BY st.last_cleanup_at,t.id LIMIT 20`, capacitySetting("SCRAPIO_CO_CHANGE_RETENTION_DAYS", 30, 1, 3650)).Find(&tables)
	s.Close()
	if e != nil {
		return e
	}
	for _, table := range tables {
		if e = cleanupTableChanges(ctx, table); e != nil {
			return e
		}
	}
	return nil
}
func cleanupTableChanges(ctx context.Context, table DataTable) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	if _, e := s.Exec("SELECT pg_advisory_xact_lock(hashtextextended('co-subscription-graph',0))"); e != nil {
		return e
	}
	stream, e := lockTableStream(s, table.ID)
	if e != nil {
		return e
	}
	rows, e := s.QueryString(`SELECT COALESCE(MIN(cursor),?) AS cursor FROM co_subscriptions WHERE table_id=? AND status<>'retired'`, stream.Sequence, table.ID)
	if e != nil {
		return e
	}
	var cursor int64
	if _, e = fmt.Sscan(rows[0]["cursor"], &cursor); e != nil {
		return e
	}
	changes := []RecordChange{}
	if e = s.Where("table_id=? AND sequence>? AND sequence<=?", table.ID, stream.RetainedAfter, cursor).OrderBy("sequence").Limit(1000).Find(&changes); e != nil {
		return e
	}
	until := stream.RetainedAfter
	var freed int64
	cutoff := time.Now().Add(-time.Duration(capacitySetting("SCRAPIO_CO_CHANGE_RETENTION_DAYS", 30, 1, 3650)) * 24 * time.Hour)
	for _, change := range changes {
		if change.Sequence != until+1 || !change.CreatedAt.Before(cutoff) {
			break
		}
		until = change.Sequence
		freed += change.AccountedBytes
	}
	if until == stream.RetainedAfter {
		if _, e = s.Exec("UPDATE co_table_streams SET last_cleanup_at=NOW() WHERE table_id=?", table.ID); e != nil {
			return e
		}
		return s.Commit()
	}
	if _, e = s.Exec("DELETE FROM co_record_changes WHERE table_id=? AND sequence<=?", table.ID, until); e != nil {
		return e
	}
	if _, e = s.Exec("UPDATE co_table_streams SET retained_after=?,last_cleanup_at=NOW() WHERE table_id=?", until, table.ID); e != nil {
		return e
	}
	if _, e = s.Exec("UPDATE co_storage_usage SET change_bytes=GREATEST(0,change_bytes-?) WHERE owner_id=?", freed, table.OwnerID); e != nil {
		return e
	}
	return s.Commit()
}
