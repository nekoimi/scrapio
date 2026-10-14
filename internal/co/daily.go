package co

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"os"
	"strconv"
	"time"
	"xorm.io/xorm"
)

type ScheduleRequest struct {
	TaskVersionID    string `json:"task_version_id"`
	IntervalSeconds  int    `json:"interval_seconds"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int    `json:"expected_revision"`
}
type Schedule struct {
	ID              string `xorm:"id pk"`
	OwnerID         int64  `xorm:"owner_id"`
	TaskID          string `xorm:"task_id"`
	TaskVersionID   string `xorm:"task_version_id"`
	IntervalSeconds int
	Enabled         bool
	NextRunAt       time.Time
	Revision        int
	CreatedAt       time.Time
}

func (Schedule) TableName() string { return "co_schedules" }
func TaskSchedule(ctx context.Context, owner int64, task string) (any, error) {
	s := session(ctx)
	defer s.Close()
	if _, e := getTask(s, owner, task, false); e != nil {
		return nil, e
	}
	rows, e := views(s, "SELECT * FROM co_schedules WHERE owner_id=? AND task_id=?", owner, task)
	return map[string]any{"items": rows}, e
}
func SaveSchedule(ctx context.Context, owner int64, task, key string, p ScheduleRequest) (*Operation, error) {
	if p.IntervalSeconds < 60 || p.IntervalSeconds > 2592000 {
		return nil, collector.Invalid("interval_seconds", "固定间隔需要60秒..30天（UTC计算）")
	}
	return Mutate(ctx, owner, "saveSchedule", task, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		t, e := getTask(s, owner, task, true)
		if e != nil {
			return nil, e
		}
		if t.Archived {
			return nil, conflict("任务已归档")
		}
		v := new(TaskVersion)
		ok, e := s.Where("owner_id=? AND task_id=? AND id=?", owner, task, p.TaskVersionID).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		sc := new(Schedule)
		ok, e = s.Where("owner_id=? AND task_id=?", owner, task).Get(sc)
		if e != nil {
			return nil, e
		}
		if !ok {
			if p.ExpectedRevision != 0 {
				return nil, conflict("计划不存在，请重新加载")
			}
			sc = &Schedule{ID: uuid.NewString(), OwnerID: owner, TaskID: task, TaskVersionID: p.TaskVersionID, IntervalSeconds: p.IntervalSeconds, Enabled: p.Enabled, Revision: 1, CreatedAt: time.Now(), NextRunAt: time.Now().Add(time.Duration(p.IntervalSeconds) * time.Second)}
			_, e = s.Insert(sc)
		} else {
			if sc.Revision != p.ExpectedRevision {
				return nil, conflict("计划已变化，请重新加载")
			}
			sc.TaskVersionID = p.TaskVersionID
			sc.IntervalSeconds = p.IntervalSeconds
			sc.Enabled = p.Enabled
			sc.Revision++
			sc.NextRunAt = time.Now().Add(time.Duration(p.IntervalSeconds) * time.Second)
			_, e = s.ID(sc.ID).Cols("task_version_id", "interval_seconds", "enabled", "revision", "next_run_at").Update(sc)
		}
		return map[string]any{"id": sc.ID, "revision": sc.Revision, "next_run_at": sc.NextRunAt, "enabled": sc.Enabled}, e
	})
}

// Dispatch schedules by their original due-time key. A crash after StartTask is
// recovered by that key; a changed schedule never borrows the previous result.
func TickSchedules(ctx context.Context) error {
	s := session(ctx)
	due := []Schedule{}
	e := s.Where("enabled=TRUE AND next_run_at<=NOW()").OrderBy("next_run_at,id").Limit(10).Find(&due)
	s.Close()
	if e != nil {
		return e
	}
	for _, sc := range due {
		key := fmt.Sprintf("schedule:%s:%d:%d", sc.ID, sc.Revision, sc.NextRunAt.UnixMicro())
		op, startErr := startTask(ctx, sc.OwnerID, sc.TaskID, key, sc.TaskVersionID, "formal", &sc)
		if startErr != nil {
			var typed *collector.Error
			if !errors.As(startErr, &typed) {
				return startErr
			}
		} // Uncertain commit retries the original tick, not a new trigger.
		tx := session(ctx)
		if e = tx.Begin(); e != nil {
			tx.Close()
			return e
		}
		// Task then schedule lock agrees with edits and archive. No remote calls.
		_, e = getTask(tx, sc.OwnerID, sc.TaskID, true)
		current := new(Schedule)
		if e == nil {
			_, e = tx.SQL("SELECT * FROM co_schedules WHERE id=? FOR UPDATE", sc.ID).Get(current)
		}
		if e == nil && current.Revision == sc.Revision && current.Enabled && current.NextRunAt.Equal(sc.NextRunAt) {
			status := "failed"
			data := map[string]any{}
			var run any
			if startErr != nil {
				data["error"] = collector.AsError(startErr)
			} else {
				_ = json.Unmarshal([]byte(op.Result), &data)
				status = "started"
				if data["skipped"] == true {
					status = "skipped_overlap"
				} else {
					run = data["id"]
				}
			}
			if _, e = tx.Exec(`INSERT INTO co_triggers(id,owner_id,task_id,task_version_id,schedule_id,trigger_key,kind,status,run_id,data) VALUES(?,?,?,?,?,?,'schedule',?,?,CAST(? AS jsonb)) ON CONFLICT(owner_id,trigger_key) DO NOTHING`, uuid.NewString(), sc.OwnerID, sc.TaskID, sc.TaskVersionID, sc.ID, key, status, run, collector.JSON(data)); e == nil {
				// Coalesce downtime: one original tick, next future slot, no restart storm.
				_, e = tx.Exec(`UPDATE co_schedules SET next_run_at=next_run_at + (FLOOR(EXTRACT(EPOCH FROM (NOW()-next_run_at))/interval_seconds)+1)*interval_seconds*INTERVAL '1 second' WHERE id=?`, sc.ID)
			}
		}
		if e == nil {
			e = tx.Commit()
		}
		_ = tx.Rollback()
		tx.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func refreshPolicy(m TaskManifest, stage string) string {
	// History is partitioned by version, saving policy, outgoing delivery policy.
	edges := []TaskEdge{}
	for _, e := range m.Definition.Edges {
		if e.From == stage {
			edges = append(edges, e)
		}
	}
	return collector.Hash(map[string]any{"stage": m.Stage(stage), "edges": edges})
}
func recordSuccessfulInput(s *xorm.Session, r *Run, m TaskManifest, stage string, item *WorkItem) error {
	_, e := s.Exec(`INSERT INTO co_success_history(owner_id,collector_version_id,input_hash,policy_hash,item_id,run_id) VALUES(?,?,?,?,?,?) ON CONFLICT(owner_id,collector_version_id,input_hash,policy_hash) DO UPDATE SET item_id=EXCLUDED.item_id,run_id=EXCLUDED.run_id,succeeded_at=NOW()`, r.OwnerID, m.Stage(stage).CollectorVersionID, item.InputHash, refreshPolicy(m, stage), item.ID, r.ID)
	return e
}
func skipFresh(s *xorm.Session, r *Run, m TaskManifest, stage string, inputHash string) (map[string]any, error) {
	st := m.Stage(stage)
	if r.Namespace != "formal" || st.RefreshSeconds == 0 {
		return nil, nil
	}
	rows, e := views(s, `SELECT item_id,run_id,succeeded_at FROM co_success_history WHERE owner_id=? AND collector_version_id=? AND input_hash=? AND policy_hash=? AND succeeded_at>NOW()-?*INTERVAL '1 second'`, r.OwnerID, st.CollectorVersionID, inputHash, refreshPolicy(m, stage), st.RefreshSeconds)
	if e != nil || len(rows) == 0 {
		return nil, e
	}
	return map[string]any{"code": "FRESH_INPUT_SKIPPED", "message": "该版本和策略的输入在刷新间隔内已成功；本次未访问，也未重发旧输出到下游", "evidence": json.RawMessage(rows[0]), "refresh_seconds": st.RefreshSeconds, "coverage": "skipped_with_descendants"}, nil
}
func capacitySetting(name string, fallback, min, max int) int {
	n, e := strconv.Atoi(os.Getenv(name))
	if e != nil || n < min || n > max {
		return fallback
	}
	return n
}
func reserveStorage(s *xorm.Session, owner int64, column string, amount int64) error {
	name, fallback, code := "SCRAPIO_CO_SOURCE_STORAGE_MIB", 64, "SOURCE_STORAGE_LIMIT"
	if column == "output_bytes" {
		name, fallback, code = "SCRAPIO_CO_OUTPUT_STORAGE_MIB", 256, "OUTPUT_STORAGE_LIMIT"
	} else if column == "change_bytes" {
		name, fallback, code = "SCRAPIO_CO_CHANGE_STORAGE_MIB", 128, "CHANGE_STORAGE_LIMIT"
	} else if column != "source_bytes" {
		return collector.Invalid("storage", "无效容量类型")
	}
	if _, e := s.Exec("INSERT INTO co_storage_usage(owner_id) VALUES(?) ON CONFLICT DO NOTHING", owner); e != nil {
		return e
	}
	result, e := s.Exec("UPDATE co_storage_usage SET "+column+"="+column+"+? WHERE owner_id=? AND "+column+"+?<=?", amount, owner, amount, int64(capacitySetting(name, fallback, 4, 4096))*1024*1024)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		if column == "change_bytes" {
			return collector.Fault(code, "storage", "当前账号变更事件证据容量达到上限，本页正式保存已回滚；请处理积压或结束不再需要的订阅，等待保留清理，或调整SCRAPIO_CO_CHANGE_STORAGE_MIB后恢复运行")
		}
		return collector.Fault(code, "storage", "当前账号来源/输出JSON证据容量达到上限；当前事务回滚。保留已提交来源，请调整容量或缩小任务范围")
	}
	return nil
}
func Governance(ctx context.Context, owner int64) (any, error) {
	s := session(ctx)
	defer s.Close()
	counts, e := views(s, "SELECT status,COUNT(*) AS count FROM co_runs WHERE owner_id=? GROUP BY status", owner)
	if e != nil {
		return nil, e
	}
	active, e := views(s, `SELECT r.id,r.status,r.namespace,r.input_count,r.preparing_cursor,r.output_count,v.task_id,t.name,
 (SELECT COUNT(*) FROM co_work_items w WHERE w.run_id=r.id AND w.status IN ('queued','backpressured','awaiting_confirmation')) AS backlog
 FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id JOIN co_tasks t ON t.id=v.task_id WHERE r.owner_id=? ORDER BY r.created_at DESC LIMIT 20`, owner)
	if e != nil {
		return nil, e
	}
	triggers, e := views(s, "SELECT * FROM co_triggers WHERE owner_id=? ORDER BY created_at DESC LIMIT 20", owner)
	if e != nil {
		return nil, e
	}
	tables, e := views(s, `SELECT t.id,t.name,t.current_schema_version,COUNT(r.id) AS record_count,MAX(r.last_seen_at) AS last_seen_at FROM co_data_tables t LEFT JOIN co_records r ON r.table_id=t.id WHERE t.owner_id=? GROUP BY t.id ORDER BY last_seen_at DESC NULLS LAST LIMIT 10`, owner)
	if e != nil {
		return nil, e
	}
	usage, e := views(s, `SELECT (SELECT COUNT(*) FROM co_work_items w JOIN co_runs r ON r.id=w.run_id WHERE r.owner_id=? AND w.status IN ('queued','backpressured')) AS pending_items,(SELECT COALESCE(SUM(p.total-p.next_ordinal),0) FROM co_dispatch_plans p JOIN co_runs r ON r.id=p.run_id WHERE r.owner_id=? AND p.status='pending') AS pending_dispatches,(SELECT COALESCE(source_bytes,0) FROM co_storage_usage WHERE owner_id=?) AS source_bytes,(SELECT COALESCE(output_bytes,0) FROM co_storage_usage WHERE owner_id=?) AS output_bytes`, owner, owner, owner, owner)
	if e != nil {
		return nil, e
	}
	subscriptionUsage, e := views(s, `SELECT COALESCE((SELECT change_bytes FROM co_storage_usage WHERE owner_id=?),0) AS change_bytes,(SELECT COUNT(*) FROM co_subscriptions WHERE owner_id=? AND status<>'retired') AS retained_subscriptions,(SELECT COUNT(*) FROM co_subscriptions WHERE owner_id=? AND status='blocked') AS blocked_subscriptions`, owner, owner, owner)
	if e != nil {
		return nil, e
	}
	var usageView map[string]any
	if len(usage) > 0 {
		_ = json.Unmarshal(usage[0], &usageView)
	}
	var changesView map[string]any
	if len(subscriptionUsage) > 0 {
		_ = json.Unmarshal(subscriptionUsage[0], &changesView)
	}
	for k, v := range changesView {
		usageView[k] = v
	}
	usage = []json.RawMessage{json.RawMessage(collector.JSON(usageView))}
	return map[string]any{"counts": counts, "runs": active, "triggers": triggers, "tables": tables, "usage": usage, "capacity": map[string]any{"http_slots": slots("SCRAPIO_CO_HTTP_SLOTS", 2), "browser_slots": slots("SCRAPIO_CO_BROWSER_SLOTS", 1), "site_interval_ms": capacitySetting("SCRAPIO_CO_SITE_INTERVAL_MS", 500, 0, 60000), "source_rows": sourceRowsMax, "source_bytes": 4 * 1024 * 1024, "source_storage_bytes": capacitySetting("SCRAPIO_CO_SOURCE_STORAGE_MIB", 64, 4, 4096) * 1024 * 1024, "output_storage_bytes": capacitySetting("SCRAPIO_CO_OUTPUT_STORAGE_MIB", 256, 4, 4096) * 1024 * 1024, "change_storage_bytes": capacitySetting("SCRAPIO_CO_CHANGE_STORAGE_MIB", 128, 4, 4096) * 1024 * 1024, "change_retention_days": capacitySetting("SCRAPIO_CO_CHANGE_RETENTION_DAYS", 30, 1, 3650), "subscriptions_per_owner": 20, "storage_scope": "source, immutable output and table change JSON accounting; database disk, indexes and other evidence require deployment monitoring", "max_active_runs": capacitySetting("SCRAPIO_CO_ACTIVE_RUNS", 20, 1, 100), "retention_days": capacitySetting("SCRAPIO_CO_SOURCE_RETENTION_DAYS", 30, 1, 3650), "defaults_measured": false, "cleanup_scope": "unused source assets and old delivered change prefixes; pending subscription changes, referenced inputs, outputs, outbox and history retained"}}, e
}
func CleanupSources(ctx context.Context) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	if _, e := s.Exec("SELECT pg_advisory_xact_lock(hashtextextended('co-source-retention',0))"); e != nil {
		return e
	}
	// Immutable published references protect assets forever; drafts too. Runs
	// additionally own full snapshots. Never delete outputs/dispatch/lineage.
	_, e := s.Exec(`WITH expired AS (UPDATE co_source_snapshots a SET expired=TRUE,rows_json='[]',original_content='',provenance=provenance - 'content' WHERE a.id IN (SELECT x.id FROM co_source_snapshots x WHERE NOT x.expired AND x.created_at<NOW()-?*INTERVAL '1 day' AND NOT EXISTS(SELECT 1 FROM co_tasks t WHERE t.definition -> 'source' ->> 'snapshot_id'=x.id) AND NOT EXISTS(SELECT 1 FROM co_task_versions v WHERE v.manifest -> 'definition' -> 'source' ->> 'snapshot_id'=x.id) ORDER BY x.created_at LIMIT 20) RETURNING owner_id,accounted_bytes) UPDATE co_storage_usage u SET source_bytes=GREATEST(0,u.source_bytes-freed.bytes) FROM (SELECT owner_id,SUM(accounted_bytes) AS bytes FROM expired GROUP BY owner_id) freed WHERE u.owner_id=freed.owner_id`, capacitySetting("SCRAPIO_CO_SOURCE_RETENTION_DAYS", 30, 1, 3650))
	if e != nil {
		return e
	}
	return s.Commit()
}
