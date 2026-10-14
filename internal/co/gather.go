package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"sort"
)

func gatherStage(s TaskStage) bool { return s.InputMode == "join" || s.InputMode == "batch" }
func (d TaskDefinition) Stage(id string) TaskStage {
	for _, s := range d.Stages {
		if s.ID == id {
			return s
		}
	}
	return TaskStage{}
}
func validateGatherConfig(s TaskStage, parents int) error {
	loc := "stages." + s.ID + ".gather"
	if !gatherStage(s) {
		if s.Gather != nil {
			return collector.Invalid(loc, "仅join/batch使用汇聚配置")
		}
		return nil
	}
	g := s.Gather
	if g == nil || g.MaxRows < 1 || g.MaxRows > 1000 || g.MaxBytes < 1024 || g.MaxBytes > 4*1024*1024 {
		return collector.Invalid(loc, "汇聚需设置1..1000行、1024..4194304字节预算")
	}
	if s.InputMode == "join" {
		if parents < 2 || len(g.KeyFields) < 1 || len(g.KeyFields) > 5 || (g.Missing != "reject" && g.Missing != "skip") {
			return collector.Invalid(loc, "按键Join需2..5来源、1..5匹配键，缺失策略reject/skip")
		}
		seen := map[string]bool{}
		for _, k := range g.KeyFields {
			if !graphID.MatchString(k) || seen[k] {
				return collector.Invalid(loc+".key_fields", "匹配键无效或重复")
			}
			seen[k] = true
		}
	} else if len(g.KeyFields) > 0 || g.Missing != "reject" {
		return collector.Invalid(loc, "批次模式不使用匹配键，来源不完整时拒绝执行")
	}
	return nil
}

// A gather edge carries a typed fragment; defaults and fetching are checked
// only on the complete joined/batched input. Batch collects scalar -> array.
func gatherRowDefinition(stage TaskStage, d collector.Definition) collector.Definition {
	if !gatherStage(stage) {
		return d
	}
	d.InputFields = append([]collector.Field{}, d.InputFields...)
	for i := range d.InputFields {
		f := &d.InputFields[i]
		f.Required = false
		f.Default = nil
		if stage.InputMode == "batch" {
			f.Multiple = false
		}
	}
	return d
}
func validatePartialInput(d collector.Definition, values map[string]any) (map[string]any, error) {
	return collector.ValidateInput(d, values)
}
func validateGatherMappings(d TaskDefinition, s TaskStage, down collector.Definition) error {
	if !gatherStage(s) {
		return nil
	}
	loc := "stages." + s.ID + ".gather"
	keys := map[string]bool{}
	for _, k := range s.Gather.KeyFields {
		keys[k] = true
	}
	fields := map[string]collector.Field{}
	for _, f := range down.InputFields {
		fields[f.Key] = f
	}
	for k := range keys {
		f, ok := fields[k]
		if !ok || f.Multiple || f.Nullable || f.Type == "json" {
			return collector.Invalid(loc+".key_fields", "匹配键需非null标量输入字段")
		}
	}
	providers := map[string]string{}
	for _, ed := range d.Edges {
		if ed.To != s.ID {
			continue
		}
		if len(ed.Mapping) == 0 {
			return collector.Invalid("edges."+ed.ID+".mapping", "汇聚来源至少映射一个字段")
		}
		for k := range keys {
			if _, ok := ed.Mapping[k]; !ok {
				return collector.Invalid("edges."+ed.ID+".mapping."+k, "每条Join来源都需映射匹配键")
			}
		}
		for k := range ed.Mapping {
			if keys[k] {
				continue
			}
			if providers[k] != "" {
				return collector.Invalid(loc, "字段"+k+"只能由一条来源连接提供；共享字段仅限匹配键")
			}
			if s.InputMode == "batch" && !fields[k].Multiple {
				return collector.Invalid("edges."+ed.ID+".mapping."+k, "批次映射的目标需为数组字段；固定参数请在采集器输入配置默认值")
			}
			providers[k] = ed.ID
		}
	}
	for _, f := range down.InputFields {
		if f.Required && len(f.Default) == 0 && !keys[f.Key] && providers[f.Key] == "" {
			return collector.Invalid(loc, "必填输入"+f.Key+"没有来源映射或默认值")
		}
	}
	return nil
}

type GatherRow struct {
	ID     string `xorm:"id"`
	EdgeID string `xorm:"edge_id"`
	Values string `xorm:"input_snapshot"`
}
type GatherResult struct {
	Key     string
	Values  map[string]any
	Members []string
	Status  string
	Error   *collector.Error
}

func joinedKey(s TaskStage, values map[string]any) (string, error) {
	key := []any{}
	for _, k := range s.Gather.KeyFields {
		v, ok := values[k]
		if !ok || v == nil {
			return "", collector.Invalid("gather.key_fields."+k, "匹配键缺失或null")
		}
		key = append(key, v)
	}
	return collector.Hash(key), nil
}

// Inputs are bounded and ordered by persisted source receipt ID, independent
// of dispatch polling order. Duplicate per-edge keys never form a cross product.
func gatherResults(d TaskDefinition, s TaskStage, down collector.Definition, rows []GatherRow) []GatherResult {
	groups := map[string]*GatherResult{}
	byEdge := map[string]map[string]bool{}
	edges := []TaskEdge{}
	for _, e := range d.Edges {
		if e.To == s.ID {
			edges = append(edges, e)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	if s.InputMode == "batch" {
		g := &GatherResult{Key: "batch", Values: map[string]any{}, Status: "pending"}
		groups[g.Key] = g
		for _, ed := range edges {
			for k := range ed.Mapping {
				g.Values[k] = []any{}
			}
		}
	}
	for _, row := range rows {
		values := map[string]any{}
		err := collector.Strict([]byte(row.Values), &values)
		key := "batch"
		if err == nil && s.InputMode == "join" {
			key, err = joinedKey(s, values)
		}
		if err != nil {
			key = "invalid:" + row.ID
		}
		g := groups[key]
		if g == nil {
			g = &GatherResult{Key: key, Values: map[string]any{}, Status: "pending"}
			groups[key] = g
		}
		g.Members = append(g.Members, row.ID)
		if byEdge[key] == nil {
			byEdge[key] = map[string]bool{}
		}
		if err != nil {
			g.Status = "invalid"
			g.Error = collector.AsError(err)
			continue
		}
		if s.InputMode == "join" && byEdge[key][row.EdgeID] {
			g.Status = "invalid"
			g.Error = collector.AsError(collector.Fault("JOIN_DUPLICATE_KEY", "gather", "同一来源有重复匹配键，请先消除歧义"))
		}
		byEdge[key][row.EdgeID] = true
		for k, v := range values {
			if s.InputMode == "batch" {
				g.Values[k] = append(g.Values[k].([]any), v)
			} else {
				g.Values[k] = v
			}
		}
	}
	names := []string{}
	for key := range groups {
		names = append(names, key)
	}
	sort.Strings(names)
	results := []GatherResult{}
	for _, key := range names {
		g := groups[key]
		if g.Status == "pending" && s.InputMode == "join" {
			for _, ed := range edges {
				if !byEdge[key][ed.ID] {
					g.Status = "invalid"
					if s.Gather.Missing == "skip" {
						g.Status = "skipped"
					}
					g.Error = collector.AsError(collector.Fault("JOIN_MISSING_SOURCE", "gather", "此匹配键缺少来源"))
					break
				}
			}
		}
		if g.Status == "pending" {
			v, e := collector.ValidateInput(down, g.Values)
			if e == nil {
				_, e = collector.Prepare(down, v)
			}
			if e != nil {
				g.Status = "invalid"
				g.Error = collector.AsError(e)
			} else {
				g.Values = v
			}
		}
		results = append(results, *g)
	}
	return results
}
