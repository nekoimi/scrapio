package co

import (
	"context"
	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

type ReplayRequest struct {
	EdgeID        string        `json:"edge_id,omitempty"`
	StageID       string        `json:"stage_id"`
	TaskVersionID string        `json:"task_version_id,omitempty"`
	ItemIDs       []string      `json:"item_ids,omitempty"`
	OutputIDs     []string      `json:"output_ids,omitempty"`
	Source        *ManualSource `json:"source,omitempty"`
	Confirmed     bool          `json:"confirmed"`
}

// Only the chosen node and its descendants run; no simulated upstream pass.
func pruneManifest(m TaskManifest, entry string) (TaskManifest, []string, error) {
	frozen := make(map[string]FrozenStage, len(m.Stages))
	for id, stage := range m.Stages {
		frozen[id] = stage
	}
	m.Stages = frozen
	if m.Stage(entry).ID == "" {
		return m, nil, collector.Invalid("stage_id", "阶段不存在")
	}
	keep := map[string]bool{entry: true}
	for i := 0; i < 20; i++ {
		for _, e := range m.Definition.Edges {
			if keep[e.From] {
				keep[e.To] = true
			}
		}
	}
	stages := []TaskStage{}
	upstream := []string{}
	for _, st := range m.Definition.Stages {
		if keep[st.ID] {
			st.RefreshSeconds = 0
			if st.ID == entry {
				st.InputMode = "single"
				st.Gather = nil
			}
			stages = append(stages, st)
		} else {
			upstream = append(upstream, st.ID)
			delete(m.Stages, st.ID)
		}
	}
	edges := []TaskEdge{}
	for _, e := range m.Definition.Edges {
		if keep[e.From] && keep[e.To] {
			edges = append(edges, e)
		}
	}
	// A shared Join/batch cannot silently lose required upstream branches.
	for _, ed := range m.Definition.Edges {
		if keep[ed.To] && ed.To != entry && !keep[ed.From] && gatherStage(m.Stage(ed.To)) {
			return m, nil, collector.Invalid("stage_id", "局部路径会缺少汇聚来源，请从包含全部来源的阶段重跑，或直接补采汇聚后的历史输入")
		}
	}
	m.Definition.Stages = stages
	m.Definition.Edges = edges
	m.Definition.EntryStage = entry
	m = m.ForStage(entry)
	return m, upstream, nil
}
func ReplayStage(ctx context.Context, owner int64, runID, key string, p ReplayRequest) (*Operation, error) {
	if !p.Confirmed {
		return nil, collector.Invalid("confirmed", "局部补采会再次访问并保存；它不代表全链路回归，请确认")
	}
	if p.EdgeID != "" && len(p.OutputIDs) == 0 {
		return nil, collector.Invalid("edge_id", "仅历史上游输出补采可指定连接")
	}
	count := 0
	if len(p.ItemIDs) > 0 {
		count++
	}
	if len(p.OutputIDs) > 0 {
		count++
	}
	if p.Source != nil {
		count++
	}
	if count != 1 || len(p.ItemIDs) > 100 || len(p.OutputIDs) > 100 {
		return nil, collector.Invalid("source", "请选择一类补采来源：最多100个原工作项/历史输出或已确认表快照")
	}
	return Mutate(ctx, owner, "replayStage", runID, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		original, e := getRun(s, owner, runID, false)
		if e != nil {
			return nil, e
		}
		v := new(TaskVersion)
		id := p.TaskVersionID
		if id == "" {
			id = original.TaskVersionID
		}
		ok, e := s.Where("owner_id=? AND id=?", owner, id).Get(v)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		ov := new(TaskVersion)
		ok, e = s.Where("owner_id=? AND id=?", owner, original.TaskVersionID).Get(ov)
		if e != nil {
			return nil, e
		}
		if !ok || ov.TaskID != v.TaskID {
			return nil, collector.Invalid("task_version_id", "修复版本必须属于原任务")
		}
		task, e := getTask(s, owner, v.TaskID, true)
		if e != nil {
			return nil, e
		}
		if task.Archived {
			return nil, conflict("任务已归档")
		}
		original, e = getRun(s, owner, runID, true)
		if e != nil {
			return nil, e
		}
		if !terminal(original.Status) {
			return nil, conflict("请先结束原运行并处理未知动作")
		}
		n, e := s.SQL(`SELECT COUNT(*) FROM co_runs r JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND r.namespace=? AND r.status IN ('preparing','running','paused','pausing','awaiting_confirmation')`, v.TaskID, original.Namespace).Count(new(Run))
		if e != nil {
			return nil, e
		}
		if n > 0 {
			return nil, conflict("任务已有活动运行")
		}
		var m, old TaskManifest
		if e = collector.Strict([]byte(v.Manifest), &m); e != nil {
			return nil, e
		}
		if e = collector.Strict([]byte(original.Manifest), &old); e != nil {
			return nil, e
		}
		if id == original.TaskVersionID {
			m = old
		} // Includes frozen offline evidence, never latest.
		if original.Namespace == "trial" {
			m.Trial = old.Trial
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
		}
		scopeEdges := m.Definition.Edges
		unexecuted := []string{}
		m, unexecuted, e = pruneManifest(m, p.StageID)
		if e != nil {
			return nil, e
		}
		inputs := []RunInput{}
		scope := []string{"partial_replay", "entry_stage:" + p.StageID}
		for _, ed := range scopeEdges {
			if m.Stage(ed.To).ID != "" && m.Stage(ed.From).ID == "" {
				scope = append(scope, "not_executed_edge:"+ed.ID)
			}
		}
		for _, st := range unexecuted {
			scope = append(scope, "not_executed:"+st)
		}
		if p.Source != nil {
			if p.Source.Kind != "table" {
				return nil, collector.Invalid("source", "局部表补采需要显式确认表快照")
			}
			inputs, e = frozenSourceInputs(s, owner, *p.Source, m.CollectorDefinition)
			if e != nil {
				return nil, e
			}
		} else if len(p.ItemIDs) > 0 {
			for _, id := range p.ItemIDs {
				item := new(WorkItem)
				ok, e := s.SQL("SELECT w.* FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND w.id=? AND st.stage_id=?", runID, id, p.StageID).Get(item)
				if e != nil {
					return nil, e
				}
				if !ok {
					return nil, ErrNotFound
				}
				if item.Status == "awaiting_confirmation" {
					return nil, conflict("未知动作不能自动补采")
				}
				inputs = append(inputs, RunInput{SourceKey: "replay:item:" + id, Values: item.InputValues, Source: collector.JSON(map[string]any{"kind": "historical_input", "run_id": runID, "item_id": id, "input_id": item.InputID, "target_stage": p.StageID, "task_version_id": v.ID})})
				scope = append(scope, "item:"+id)
			}
		} else {
			// Each selected historical output must use one explicit target edge.
			var full TaskManifest
			if e = collector.Strict([]byte(v.Manifest), &full); e != nil {
				return nil, e
			}
			if gatherStage(full.Stage(p.StageID)) {
				return nil, collector.Invalid("output_ids", "Join/批次不能用单条上游连接补采，请选择完整历史输入或表快照")
			}
			edge, err := replayEdge(full.Definition, p.StageID, p.EdgeID)
			if err != nil {
				return nil, err
			}
			if e = validateEdge(edge, old.ForStage(edge.From).CollectorDefinition, m.CollectorDefinition); e != nil {
				return nil, e
			}
			for _, id := range p.OutputIDs {
				output := new(SavedOutput)
				ok, e := s.SQL(`SELECT o.* FROM co_run_outputs o JOIN co_work_items w ON w.id=o.item_id JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE o.run_id=? AND o.id=? AND st.stage_id=? AND o.port=?`, runID, id, edge.From, edge.Port).Get(output)
				if e != nil {
					return nil, e
				}
				if !ok {
					return nil, collector.Fault("OUTPUT_EVIDENCE_MISSING", "replay", "历史输出缺失或不属于此上游；不能代用表最新值")
				}
				item := new(WorkItem)
				if _, e = s.ID(output.ItemID).Get(item); e != nil {
					return nil, e
				}
				var input, values map[string]any
				if e = collector.Strict([]byte(item.InputValues), &input); e != nil {
					return nil, e
				}
				if e = collector.Strict([]byte(output.Values), &values); e != nil {
					return nil, e
				}
				rows, e := mapEdge(edge, m.CollectorDefinition, input, values)
				if e != nil {
					return nil, e
				}
				for _, row := range rows {
					if row.Error != nil {
						return nil, row.Error
					}
					if row.Decision == "filtered" {
						continue
					}
					inputs = append(inputs, RunInput{SourceKey: "replay:output:" + id + ":" + row.ElementKey, Values: collector.JSON(row.Values), Source: collector.JSON(map[string]any{"kind": "historical_output", "run_id": runID, "output_id": id, "edge_id": edge.ID, "port": edge.Port, "mapping_hash": collector.Hash(edge), "element_key": row.ElementKey, "target_stage": p.StageID, "task_version_id": v.ID})})
				}
				scope = append(scope, "output:"+id)
			}
		}
		seen := map[string]bool{}
		for i, in := range inputs {
			if seen[in.SourceKey] {
				return nil, collector.Invalid("source", "补采来源重复")
			}
			seen[in.SourceKey] = true
			var values map[string]any
			if e = collector.Strict([]byte(in.Values), &values); e != nil {
				return nil, e
			}
			values, e = collector.ValidateInput(m.CollectorDefinition, values)
			if e != nil {
				return nil, e
			}
			inputs[i].Values = collector.JSON(values)
			if _, e = collector.Prepare(m.CollectorDefinition, values); e != nil {
				return nil, e
			}
		}
		if p.Source != nil {
			m.Definition.Source = *p.Source
		} else {
			if len(inputs) > 100 {
				return nil, collector.Invalid("source", "历史证据展开后最多100条补采输入；请选择更小范围")
			}
			values := []map[string]any{}
			for _, in := range inputs {
				var value map[string]any
				if e = collector.Strict([]byte(in.Values), &value); e != nil {
					return nil, e
				}
				values = append(values, value)
			}
			m.Definition.Source = ManualSource{Kind: "manual", Inputs: values}
		} // Inputs and provenance are already frozen, no latest reads at execution.
		m.Hash = ""
		m.Hash = collector.Hash(m)
		v.Manifest = collector.JSON(m)
		return newRun(s, owner, *v, original.Namespace, runID, inputs, scope)
	})
}
