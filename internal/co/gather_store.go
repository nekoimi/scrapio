package co

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

type gatherGroup struct {
	ID      string `xorm:"id"`
	Key     string `xorm:"group_key"`
	Values  string `xorm:"jsonb values_json"`
	Members string `xorm:"jsonb members"`
}

func (*gatherGroup) TableName() string { return "co_gather_groups" }

// Stored receipts are the buffer. They do not consume runnable queue capacity,
// so a one-slot queue can still finish every producer before releasing a batch.
func bufferGather(s *xorm.Session, r *Run, m TaskManifest, stage StageRun, row MappingPreview) (string, any, error) {
	g := m.Stage(stage.StageID).Gather
	rows, e := s.QueryString(`SELECT COUNT(*) AS n,COALESCE(SUM(octet_length(input_snapshot::text)),0) AS bytes,octet_length(CAST(? AS jsonb)::text) AS added FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.to_stage=? AND d.status='buffered'`, collector.JSON(row.Values), r.ID, stage.StageID)
	if e != nil {
		return "", nil, e
	}
	var n, b, added int
	fmt.Sscan(rows[0]["n"], &n)
	fmt.Sscan(rows[0]["bytes"], &b)
	fmt.Sscan(rows[0]["added"], &added)
	if n >= g.MaxRows || b+added > g.MaxBytes {
		return "limited", map[string]string{"code": "GATHER_BUFFER_BUDGET", "message": "汇聚缓存预算已耗尽；不执行不完整批次"}, nil
	}
	return "buffered", map[string]any{}, nil
}

func settleGather(s *xorm.Session, r *Run, m TaskManifest, st StageRun, closed bool, reason string) (int, int, int, error) {
	cfg := m.Stage(st.StageID)
	if closed && !st.GatherSealed {
		var rows []GatherRow
		if e := s.SQL(`SELECT d.id,d.edge_id,d.input_snapshot FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.to_stage=? AND d.status='buffered' ORDER BY d.id`, r.ID, st.StageID).Find(&rows); e != nil {
			return 0, 0, 0, e
		}
		errors, e := s.SQL(`SELECT COUNT(*) FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.to_stage=? AND d.status IN ('invalid','limited')`, r.ID, st.StageID).Count(new(DispatchPlan))
		if e != nil {
			return 0, 0, 0, e
		}
		results := gatherResults(m.Definition, cfg, m.ForStage(st.StageID).CollectorDefinition, rows)
		if errors > 0 || reason != "upstream_succeeded" {
			members := []string{}
			for _, row := range rows {
				members = append(members, row.ID)
			}
			results = []GatherResult{{Key: "incomplete_sources", Values: map[string]any{}, Members: members, Status: "invalid", Error: collector.AsError(collector.Fault("GATHER_INCOMPLETE_SOURCES", "gather", "来源失败、受限或映射无效；汇聚未执行，请修复后重新运行完整上游"))}}
		}
		// Defaults may enlarge many small fragments into large complete inputs.
		// Bound the sealed snapshot too, before releasing any resulting work.
		sealedBytes := 0
		for _, g := range results {
			sealedBytes += len(collector.JSON(g.Values)) + len(collector.JSON(g.Members))
		}
		if sealedBytes > cfg.Gather.MaxBytes {
			results = []GatherResult{{Key: "sealed_budget", Values: map[string]any{}, Status: "limited", Error: collector.AsError(collector.Fault("GATHER_SEALED_BUDGET", "gather", "收齐后的完整输入与来源快照超过字节预算，请缩小范围或提高预算"))}}
		}
		for _, g := range results {
			if _, e = s.Exec(`INSERT INTO co_gather_groups(id,run_id,stage_run_id,group_key,values_json,members,status,error) VALUES(?,?,?,?,CAST(? AS jsonb),CAST(? AS jsonb),?,CAST(? AS jsonb))`, uuid.NewString(), r.ID, st.ID, g.Key, collector.JSON(g.Values), collector.JSON(g.Members), g.Status, collector.JSON(g.Error)); e != nil {
				return 0, 0, 0, e
			}
		}
		if _, e = s.Exec("UPDATE co_stage_runs SET gather_sealed=TRUE WHERE id=?", st.ID); e != nil {
			return 0, 0, 0, e
		}
		if e = event(s, r, "gather_sealed", "", map[string]any{"stage_id": st.StageID, "mode": cfg.InputMode, "groups": len(results), "rows": len(rows)}); e != nil {
			return 0, 0, 0, e
		}
	}
	if closed && st.BlockedReason == "" {
		groups := []gatherGroup{}
		if e := s.Where("run_id=? AND stage_run_id=? AND status='pending'", r.ID, st.ID).OrderBy("group_key").Limit(10).Find(&groups); e != nil {
			return 0, 0, 0, e
		}
		pending, e := s.Where("run_id=? AND status IN ('queued','running')", r.ID).Count(new(WorkItem))
		if e != nil {
			return 0, 0, 0, e
		}
		total, e := s.Where("run_id=?", r.ID).Count(new(WorkItem))
		if e != nil {
			return 0, 0, 0, e
		}
		for _, g := range groups {
			child, e := mergedItem(s, r.ID, st.ID, cfg, g.Values)
			if e != nil {
				return 0, 0, 0, e
			}
			merged := child != ""
			if !merged && int(total)+r.InputCount-r.PreparingCursor >= m.Definition.Budget.MaxWorkItems {
				if _, e = s.Exec("UPDATE co_gather_groups SET status='limited',error=CAST(? AS jsonb) WHERE id=?", `{"code":"WORK_ITEM_BUDGET"}`, g.ID); e != nil {
					return 0, 0, 0, e
				}
				continue
			}
			if !merged && int(pending) >= m.Definition.Budget.MaxPending {
				break
			}
			inputID := uuid.NewString()
			sourceKey := "gather:" + st.StageID + ":" + g.Key
			if _, e = s.Exec(`INSERT INTO co_run_inputs(id,run_id,ordinal,source_key,values_json,source,materialized_at) SELECT ?,?,COALESCE(MAX(ordinal),-1)+1,?,CAST(? AS jsonb),CAST(? AS jsonb),NOW() FROM co_run_inputs WHERE run_id=?`, inputID, r.ID, sourceKey, g.Values, collector.JSON(map[string]any{"kind": "gather", "group_id": g.ID, "stage_id": st.StageID, "mode": cfg.InputMode, "key": g.Key}), r.ID); e != nil {
				return 0, 0, 0, e
			}
			if !merged {
				child, e = createInputItem(s, r.ID, st.ID, cfg, m.ForStage(st.StageID).CollectorDefinition, RunInput{ID: inputID, SourceKey: sourceKey, Values: g.Values})
				if e != nil {
					return 0, 0, 0, e
				}
				pending++
				total++
			}
			if e = attachInputSource(s, r.ID, child, inputID, merged); e != nil {
				return 0, 0, 0, e
			}
			members := []string{}
			if e = json.Unmarshal([]byte(g.Members), &members); e != nil {
				return 0, 0, 0, e
			}
			// Attach every source receipt as lineage in the same transaction as work.
			for _, id := range members {
				if _, e = s.Exec(`INSERT INTO co_lineage(target_item_id,run_id,source_item_id,source_output_id,namespace,edge_id,mapping_hash,element_key) SELECT ?,d.run_id,o.item_id,o.id,o.namespace,d.edge_id,p.mapping_hash,d.element_key FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id JOIN co_run_outputs o ON o.id=d.output_id WHERE d.run_id=? AND d.id=? ON CONFLICT DO NOTHING`, child, r.ID, id); e != nil {
					return 0, 0, 0, e
				}
			}
			if _, e = s.Exec("UPDATE co_gather_groups SET status='delivered',child_item_id=? WHERE id=?", child, g.ID); e != nil {
				return 0, 0, 0, e
			}
		}
	}
	rows, e := s.QueryString("SELECT status,COUNT(*) AS n FROM co_gather_groups WHERE run_id=? AND stage_run_id=? GROUP BY status", r.ID, st.ID)
	if e != nil {
		return 0, 0, 0, e
	}
	p, b, l := 0, 0, 0
	for _, row := range rows {
		var n int
		fmt.Sscan(row["n"], &n)
		switch row["status"] {
		case "pending":
			p += n
		case "invalid", "skipped":
			b += n
		case "limited":
			l += n
		}
	}
	return p, b, l, nil
}
