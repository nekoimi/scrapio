package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"net/url"
	"time"
	"xorm.io/xorm"
)

type DispatchPlan struct {
	ID          string `xorm:"id pk"`
	RunID       string `xorm:"run_id"`
	Namespace   string
	OutputID    string `xorm:"output_id"`
	EdgeID      string `xorm:"edge_id"`
	FromStage   string
	ToStage     string
	MappingHash string
	RowsJSON    string `xorm:"jsonb rows_json"`
	NextOrdinal int
	Total       int
	Status      string
}

func (DispatchPlan) TableName() string { return "co_dispatch_plans" }
func dispatchLimit(m TaskManifest) int {
	if m.Definition.Budget.MaxDispatches > 0 {
		return m.Definition.Budget.MaxDispatches
	}
	return 10000
}
func siteKey(values map[string]any) string {
	u, _ := url.Parse(fmt.Sprint(values["url"]))
	if u == nil {
		return ""
	}
	return u.Hostname()
}
func executionSiteInput(d collector.Definition, raw string) string {
	var input map[string]any
	_ = json.Unmarshal([]byte(raw), &input)
	return executionSite(d, input)
}
func executionSite(d collector.Definition, values map[string]any) string {
	req, e := collector.Prepare(d, values)
	if e != nil {
		return siteKey(values)
	}
	return siteKey(map[string]any{"url": req.URL})
}
func registerOutput(s *xorm.Session, r *Run, c *WorkClaim, out SavedOutput) error {
	var output, input map[string]any
	if e := json.Unmarshal([]byte(out.Values), &output); e != nil {
		return e
	}
	if e := json.Unmarshal([]byte(c.Item.InputValues), &input); e != nil {
		return e
	}
	for _, edge := range c.Manifest.Definition.Edges {
		if edge.From != c.StageID || edge.Port != out.Port {
			continue
		}
		rows := []MappingPreview{}
		emit := edge.Emit == "valid" || edge.Emit == "created" && out.Decision == "created" || edge.Emit == "changed" && (out.Decision == "created" || out.Decision == "updated")
		if emit {
			var e error
			rows, e = mapEdge(edge, c.Manifest.ForStage(edge.To).CollectorDefinition, input, output)
			if e != nil {
				rows = []MappingPreview{{Decision: "invalid", Error: collector.AsError(e), ElementKey: "mapping-error"}}
			}
		} else {
			rows = []MappingPreview{{Decision: "filtered", ElementKey: "emit-policy"}}
		}
		raw := collector.JSON(rows)
		if len(raw) > 512*1024 {
			return collector.Fault("DISPATCH_PAYLOAD_BUDGET", "edges."+edge.ID, "单输出映射快照超过512KiB，请缩小数组或字段")
		}
		p := DispatchPlan{ID: uuid.NewString(), RunID: r.ID, Namespace: r.Namespace, OutputID: out.ID, EdgeID: edge.ID, FromStage: edge.From, ToStage: edge.To, MappingHash: collector.Hash(edge), RowsJSON: raw, Total: len(rows), Status: "pending"}
		if len(rows) == 0 {
			p.Status = "delivered"
		}
		if _, e := s.Insert(&p); e != nil {
			return e
		}
	}
	return nil
}

// Dispatch and closure share the run lock. Input snapshots, lineage and outbox
// receipts commit together; a crash can only leave a whole pending plan.
func DispatchRuns(ctx context.Context) error {
	s := session(ctx)
	defer s.Close()
	rows, e := s.QueryString(`SELECT id FROM co_runs WHERE status IN ('preparing','running') ORDER BY last_dispatch_at NULLS FIRST,created_at LIMIT 20`)
	if e != nil {
		return e
	}
	for _, row := range rows {
		if e = dispatchRun(ctx, row["id"]); e != nil {
			return e
		}
	}
	return nil
}
func dispatchRun(ctx context.Context, id string) error {
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
	if r.Status != "running" && r.Status != "preparing" {
		return s.Commit()
	}
	if _, e = s.Exec("UPDATE co_runs SET last_dispatch_at=NOW() WHERE id=?", id); e != nil {
		return e
	}
	var m TaskManifest
	if e = collector.Strict([]byte(r.Manifest), &m); e != nil {
		return e
	}
	if time.Since(r.CreatedAt) > time.Duration(m.Definition.Budget.MaxDurationMS)*time.Millisecond {
		return s.Commit()
	} // prepareRun closes budget and preserves pending reasons.
	// Stable order is also used by graph closure. No lock is held across fetching.
	stages := []StageRun{}
	if e = s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? ORDER BY stage_id FOR UPDATE", id).Find(&stages); e != nil {
		return e
	}
	stageByID := map[string]StageRun{}
	for _, st := range stages {
		stageByID[st.StageID] = st
	}
	pending, e := s.Where("run_id=? AND status IN ('queued','running')", id).Count(new(WorkItem))
	if e != nil {
		return e
	}
	total, e := s.Where("run_id=?", id).Count(new(WorkItem))
	if e != nil {
		return e
	}
	capacity := m.Definition.Budget.MaxPending - int(pending)
	if capacity > 10 {
		capacity = 10
	}
	plans := []DispatchPlan{}
	if e = s.SQL("SELECT p.* FROM co_dispatch_plans p JOIN co_stage_runs st ON st.run_id=p.run_id AND st.stage_id=p.to_stage WHERE p.run_id=? AND p.status='pending' AND st.blocked_reason='' ORDER BY st.last_claim_at NULLS FIRST,p.created_at,p.id LIMIT 10 FOR UPDATE OF p", id).Find(&plans); e != nil {
		return e
	}
	progressed := 0
	for _, p := range plans {
		var rows []MappingPreview
		if e = json.Unmarshal([]byte(p.RowsJSON), &rows); e != nil {
			return e
		}
		stage := stageByID[p.ToStage]
		if stage.InputClosed {
			return collector.Fault("DISPATCH_AFTER_CLOSE", "dispatch", "目标阶段已关闭，拒绝丢弃待分发数据")
		}
		for p.NextOrdinal < p.Total && progressed < 10 {
			row := rows[p.NextOrdinal]
			status := row.Decision
			var child any
			var problem any = map[string]any{}
			if row.Error != nil {
				problem = row.Error
			}
			if status == "ready" {
				childID, err := mergedItem(s, id, stage.ID, m.Stage(p.ToStage), collector.JSON(row.Values))
				if err != nil {
					return err
				}
				merged := childID != ""
				if !merged && int(total)+r.InputCount-r.PreparingCursor >= m.Definition.Budget.MaxWorkItems {
					status = "limited"
					problem = map[string]string{"code": "WORK_ITEM_BUDGET"}
				}
				if status == "ready" && !merged && capacity <= 0 {
					break
				}
				if status == "ready" {
					sourceKey := p.EdgeID + ":" + p.OutputID + ":" + row.ElementKey
					inputID := uuid.NewString()
					source := map[string]any{"kind": "output", "output_id": p.OutputID, "edge_id": p.EdgeID, "mapping_hash": p.MappingHash, "element_key": row.ElementKey}
					// Root ordinals stay [0,input_count); child ordinals are assigned under the run lock.
					if _, e = s.Exec(`INSERT INTO co_run_inputs(id,run_id,ordinal,source_key,values_json,source,materialized_at) SELECT ?,?,COALESCE(MAX(ordinal),-1)+1,?,CAST(? AS jsonb),CAST(? AS jsonb),NOW() FROM co_run_inputs WHERE run_id=?`, inputID, id, sourceKey, collector.JSON(row.Values), collector.JSON(source), id); e != nil {
						return e
					}
					if !merged {
						childID, e = createInputItem(s, id, stage.ID, m.Stage(p.ToStage), m.ForStage(p.ToStage).CollectorDefinition, RunInput{ID: inputID, SourceKey: sourceKey, Values: collector.JSON(row.Values)})
						if e != nil {
							return e
						}
						capacity--
						total++
					}
					if e = attachInputSource(s, id, childID, inputID, merged); e != nil {
						return e
					}
					if _, e = s.Exec(`INSERT INTO co_lineage(target_item_id,run_id,source_item_id,source_output_id,namespace,edge_id,mapping_hash,element_key) SELECT ?,?,item_id,id,namespace,?,?,? FROM co_run_outputs WHERE run_id=? AND namespace=? AND id=?`, childID, id, p.EdgeID, p.MappingHash, row.ElementKey, id, r.Namespace, p.OutputID); e != nil {
						return e
					}
					child = childID
					status = "delivered"
					if merged {
						status = "merged"
					}
				}
			}
			if _, e = s.Exec(`INSERT INTO co_dispatches(id,run_id,plan_id,output_id,edge_id,expansion_ordinal,element_key,status,input_snapshot,error,child_item_id) VALUES(?,?,?,?,?,?,?,?,CAST(? AS jsonb),CAST(? AS jsonb),?) ON CONFLICT(output_id,edge_id,expansion_ordinal) DO NOTHING`, uuid.NewString(), id, p.ID, p.OutputID, p.EdgeID, p.NextOrdinal, row.ElementKey, status, collector.JSON(row.Values), collector.JSON(problem), child); e != nil {
				return e
			}
			p.NextOrdinal++
			progressed++
		}
		status := "pending"
		if p.NextOrdinal == p.Total {
			status = "delivered"
		}
		if _, e = s.Exec("UPDATE co_dispatch_plans SET next_ordinal=?,status=? WHERE id=?", p.NextOrdinal, status, p.ID); e != nil {
			return e
		}
	}
	// Unblock only within remaining capacity; producers yield to downstream first.
	if capacity > 0 {
		_, e = s.Exec(`UPDATE co_work_items SET status='queued' WHERE id IN (SELECT w.id FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND w.status='backpressured' AND st.blocked_reason='' AND NOT EXISTS(SELECT 1 FROM co_dispatch_plans p JOIN co_run_outputs o ON o.id=p.output_id WHERE p.run_id=w.run_id AND p.from_stage=st.stage_id AND p.status='pending') ORDER BY w.created_at,w.id LIMIT ?)`, id, capacity)
		if e != nil {
			return e
		}
	}
	if progressed > 0 {
		if e = event(s, r, "dispatch_progress", "", map[string]any{"processed": progressed, "queue_capacity": capacity}); e != nil {
			return e
		}
	}
	if e = settle(s, r); e != nil {
		return e
	}
	return s.Commit()
}

func graphSettle(s *xorm.Session, r *Run) error {
	if terminal(r.Status) {
		return nil
	}
	var m TaskManifest
	if e := collector.Strict([]byte(r.Manifest), &m); e != nil {
		return e
	}
	stages := []StageRun{}
	if e := s.SQL("SELECT * FROM co_stage_runs WHERE run_id=? ORDER BY stage_id FOR UPDATE", r.ID).Find(&stages); e != nil {
		return e
	}
	states := map[string]string{}
	aggregate := map[string]int{}
	blocked, active, awaiting := false, 0, 0
	// Acquire locks in stable order, then calculate in topological order once.
	byID := map[string]StageRun{}
	for _, st := range stages {
		byID[st.StageID] = st
	}
	ordered := []StageRun{}
	queue := []string{m.Definition.EntryStage}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, byID[id])
		for _, edge := range m.Definition.Edges {
			if edge.From == id {
				queue = append(queue, edge.To)
			}
		}
	}
	for _, st := range ordered {
		counts := map[string]int{}
		rows, e := s.QueryString("SELECT status,COUNT(*) AS n FROM co_work_items WHERE stage_run_id=? GROUP BY status", st.ID)
		if e != nil {
			return e
		}
		for _, row := range rows {
			var n int
			fmt.Sscan(row["n"], &n)
			counts[row["status"]] = n
		}
		closed := st.InputClosed
		reason := st.CloseReason
		if !closed && st.StageID != m.Definition.EntryStage {
			for _, edge := range m.Definition.Edges {
				if edge.To == st.StageID && terminal(states[edge.From]) {
					closed = true
					reason = "upstream_" + states[edge.From]
					break
				}
			}
		}
		n, e := s.Table("co_dispatch_plans").Where("run_id=? AND from_stage=? AND status='pending'", r.ID, st.StageID).Count()
		if e != nil {
			return e
		}
		bad, e := s.SQL(`SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.from_stage=? AND d.status IN ('invalid','limited')`, r.ID, st.StageID).Count(new(DispatchPlan))
		if e != nil {
			return e
		}
		limited, e := s.SQL(`SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.from_stage=? AND d.status='limited'`, r.ID, st.StageID).Count(new(DispatchPlan))
		if e != nil {
			return e
		}
		status := "waiting_upstream"
		if closed || len(counts) > 0 {
			status = "running"
		}
		if n > 0 {
			status = "dispatching"
		}
		if st.BlockedReason != "" {
			status = "paused"
		}
		if counts["awaiting_confirmation"] > 0 {
			status = "awaiting_confirmation"
		}
		if closed && counts["queued"]+counts["running"]+counts["backpressured"]+counts["awaiting_confirmation"] == 0 && n == 0 && st.BlockedReason == "" {
			status = "succeeded"
			if counts["limited"] > 0 || limited > 0 || reason == "upstream_limited" {
				status = "limited"
			}
			if counts["partial"] > 0 || counts["skipped"] > 0 || bad-limited > 0 || reason == "upstream_partial" || reason == "upstream_failed" {
				status = "partial"
			}
			if counts["failed"] > 0 {
				status = "failed"
				if counts["succeeded"]+counts["partial"]+counts["limited"] > 0 {
					status = "partial"
				}
			}
		}
		states[st.StageID] = status
		if _, e = s.Exec("UPDATE co_stage_runs SET input_closed=?,close_reason=?,status=?,counters=CAST(? AS jsonb) WHERE id=?", closed, reason, status, collector.JSON(counts), st.ID); e != nil {
			return e
		}
	}
	allDone := true
	for _, st := range stages {
		if !terminal(states[st.StageID]) {
			allDone = false
		}
		if st.BlockedReason != "" {
			blocked = true
		}
	}
	rows, e := s.QueryString("SELECT status,COUNT(*) AS n FROM co_work_items WHERE run_id=? GROUP BY status", r.ID)
	if e != nil {
		return e
	}
	for _, row := range rows {
		var n int
		fmt.Sscan(row["n"], &n)
		aggregate[row["status"]] = n
	}
	active = aggregate["running"]
	awaiting = aggregate["awaiting_confirmation"]
	next := r.Status
	if r.Status == "pausing" && active == 0 {
		next = "paused"
	} else if r.Status == "running" || r.Status == "preparing" {
		if allDone {
			next = "succeeded"
			if aggregate["skipped"] > 0 {
				next = "partial"
			}
			for _, status := range states {
				if status == "limited" && next == "succeeded" {
					next = "limited"
				}
				if status == "partial" || status == "failed" {
					next = "partial"
				}
			}
			if aggregate["failed"] > 0 && r.OutputCount == 0 {
				next = "failed"
			}
		}
		if !allDone && active == 0 {
			if awaiting > 0 {
				next = "awaiting_confirmation"
			} else if blocked { // Leave unaffected branches runnable before pausing the run.
				n, e := s.SQL(`SELECT COUNT(*) FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND w.status IN ('queued','backpressured') AND st.blocked_reason='' AND NOT EXISTS(SELECT 1 FROM co_dispatch_plans p WHERE p.run_id=w.run_id AND p.from_stage=st.stage_id AND p.status='pending')`, r.ID).Count(new(WorkItem))
				if e != nil {
					return e
				}
				if n == 0 {
					dispatchable, e := s.SQL("SELECT COUNT(*) FROM co_dispatch_plans p JOIN co_stage_runs st ON st.run_id=p.run_id AND st.stage_id=p.to_stage WHERE p.run_id=? AND p.status='pending' AND st.blocked_reason=''", r.ID).Count(new(DispatchPlan))
					if e != nil {
						return e
					}
					if dispatchable == 0 {
						next = "paused"
					}
				}
			}
		}
	}
	if next == r.Status {
		return nil
	}
	r.Status = next
	if _, e = s.Exec("UPDATE co_runs SET status=?,finished_at=CASE WHEN ? IN ('succeeded','partial','failed','limited','cancelled') THEN NOW() ELSE NULL END WHERE id=?", next, next, r.ID); e != nil {
		return e
	}
	return event(s, r, next, "", map[string]any{"items": aggregate, "outputs": r.OutputCount, "stages": states})
}
