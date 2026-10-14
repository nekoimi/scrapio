package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nekoimi/scrapio/internal/collector"
	"regexp"
	"strings"
)

type TaskEdge struct {
	ID      string                       `json:"id"`
	From    string                       `json:"from"`
	To      string                       `json:"to"`
	Port    string                       `json:"port"`
	Emit    string                       `json:"emit"`
	Mapping map[string]collector.Binding `json:"mapping"`
	Filter  *EdgeFilter                  `json:"filter,omitempty"`
	Expand  *EdgeExpand                  `json:"expand,omitempty"`
}
type EdgeFilter struct {
	Path  string          `json:"path"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}
type EdgeExpand struct {
	Path string `json:"path"`
}

var graphID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var edgeToken = regexp.MustCompile(`\{\{(output\.[a-z][a-z0-9_]*|input\.[a-z][a-z0-9_]*|element)\}\}`)

func (d TaskDefinition) validateGraph() error {
	if d.DefinitionVersion != "task.v1" || len(d.Stages) < 1 || len(d.Stages) > 20 || len(d.Edges) > 60 {
		return collector.Invalid("stages", "需要1..20阶段、最多60条连接的单入口无环DAG；多来源阶段需显式选择union/join/batch")
	}
	nodes := map[string]bool{}
	tables := map[string]string{}
	for i, s := range d.Stages {
		loc := fmt.Sprintf("stages.%d", i)
		if !graphID.MatchString(s.ID) || nodes[s.ID] {
			return collector.Invalid(loc+".id", "阶段ID无效或重复")
		}
		nodes[s.ID] = true
		if strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 || s.CollectorVersionID == "" {
			return collector.Invalid(loc, "需要阶段名及固定采集器版本")
		}
		if s.OnError != "continue" && s.OnError != "pause_stage" {
			return collector.Invalid(loc+".on_error", "需要continue/pause_stage")
		}
		if len(s.AdditionalSaves) > 4 {
			return collector.Invalid(loc+".additional_saves", "最多4个附加端口绑定")
		}
		ports := map[string]bool{}
		for _, p := range s.Saves() {
			if !graphID.MatchString(p.Port) || ports[p.Port] {
				return collector.Invalid(loc+".additional_saves", "端口名无效或重复")
			}
			ports[p.Port] = true
			if e := validateSaveMode(p.Save); e != nil {
				return e
			}
			if passThrough(p.Save) {
				continue
			}
			if prior := tables[p.Save.TableID]; prior != "" {
				return collector.Invalid(loc+".save.table_id", "阶段/端口"+prior+"已绑定同表；请使用独立表")
			}
			tables[p.Save.TableID] = s.ID + "/" + p.Port
		}
	}
	if !nodes[d.EntryStage] {
		return collector.Invalid("entry_stage", "入口阶段不存在")
	}
	parents := map[string]int{}
	connections := map[string]bool{}
	ids := map[string]bool{}
	for i, e := range d.Edges {
		loc := fmt.Sprintf("edges.%d", i)
		if !graphID.MatchString(e.ID) || ids[e.ID] {
			return collector.Invalid(loc+".id", "连接ID无效或重复")
		}
		ids[e.ID] = true
		if !nodes[e.From] || !nodes[e.To] || e.From == e.To || e.To == d.EntryStage {
			return collector.Invalid(loc, "连接节点缺失、自连接或入口有上游")
		}
		if !graphID.MatchString(e.Port) || (e.Emit != "valid" && e.Emit != "created" && e.Emit != "changed") {
			return collector.Invalid(loc, "请选择已声明的输出端口；传递策略需valid/created/changed")
		}
		signature := e.From + ":" + e.To + ":" + e.Port
		if connections[signature] {
			return collector.Invalid(loc, "重复连接：同一来源阶段、端口和目标仅能连接一次")
		}
		connections[signature] = true
		parents[e.To]++
		for _, st := range d.Stages {
			if st.ID == e.From {
				b, err := st.Binding(e.Port)
				if err != nil {
					return err
				}
				if passThrough(b) && e.Emit != "valid" {
					return collector.Invalid(loc+".emit", "仅传递端口没有业务记录新增/变化决策，传递策略需valid")
				}
			}
		}
	}
	for _, st := range d.Stages {
		if st.InputMode != "" && st.InputMode != "single" && st.InputMode != "union" && !gatherStage(st) {
			return collector.Invalid("stages."+st.ID+".input_mode", "输入模式需single/union/join/batch")
		}
		if e := validateGatherConfig(st, parents[st.ID]); e != nil {
			return e
		}
		if st.ID == d.EntryStage {
			if st.InputMode != "" && st.InputMode != "single" {
				return collector.Invalid("stages."+st.ID+".input_mode", "入口阶段使用根输入，不使用汇聚模式")
			}
			continue
		}
		if parents[st.ID] == 0 || parents[st.ID] > 5 {
			return collector.Invalid("stages."+st.ID, "非入口阶段需1..5条上游连接")
		}
		if parents[st.ID] > 1 && st.InputMode != "union" && !gatherStage(st) {
			return collector.Invalid("stages."+st.ID+".input_mode", "多个上游需显式选择联合输入、按键配对或批次收齐")
		}
	}
	_, err := d.TopologicalStages()
	if err != nil {
		return err
	}
	return nil
}
func edgeField(path string, up collector.Definition, expand *EdgeExpand) (collector.Field, bool) {
	var fields []collector.Field
	key := ""
	if strings.HasPrefix(path, "output.") {
		fields = up.Output.Fields
		key = strings.TrimPrefix(path, "output.")
	}
	if strings.HasPrefix(path, "input.") {
		fields = up.InputFields
		key = strings.TrimPrefix(path, "input.")
	}
	if path == "element" && expand != nil {
		f, ok := edgeField(expand.Path, up, nil)
		f.Multiple = false
		f.Nullable = false
		return f, ok
	}
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
	}
	return collector.Field{}, false
}
func validateEdge(e TaskEdge, up, down collector.Definition) error {
	loc := "edges." + e.ID
	var err error
	up, err = up.ForPort(e.Port)
	if err != nil {
		return collector.Invalid(loc+".port", err.Error())
	}
	if e.Expand != nil {
		f, ok := edgeField(e.Expand.Path, up, nil)
		if !ok || !f.Multiple || !strings.HasPrefix(e.Expand.Path, "output.") {
			return collector.Invalid(loc+".expand", "展开仅支持已声明的输出数组字段")
		}
	}
	if e.Filter != nil {
		if _, ok := edgeField(e.Filter.Path, up, e.Expand); !ok {
			return collector.Invalid(loc+".filter", "过滤字段不存在")
		}
		if e.Filter.Op != "exists" && e.Filter.Op != "equal" && e.Filter.Op != "not_equal" {
			return collector.Invalid(loc+".filter", "仅支持exists/equal/not_equal")
		}
		if e.Filter.Op != "exists" && len(e.Filter.Value) == 0 {
			return collector.Invalid(loc+".filter.value", "请指定比较值")
		}
	}
	target := map[string]collector.Field{}
	for _, f := range down.InputFields {
		target[f.Key] = f
		if f.Required && len(f.Default) == 0 {
			if _, ok := e.Mapping[f.Key]; !ok {
				return collector.Invalid(loc+".mapping."+f.Key, "下游必填输入缺少映射")
			}
		}
	}
	for key, b := range e.Mapping {
		f, ok := target[key]
		if !ok {
			return collector.Invalid(loc+".mapping."+key, "下游没有此输入字段")
		}
		switch b.Kind {
		case "field":
			src, ok := edgeField(b.Path, up, e.Expand)
			if !ok || src.Type != f.Type || src.Multiple != f.Multiple || (!f.Nullable && src.Nullable) {
				return collector.Invalid(loc+".mapping."+key, "字段不存在或类型/数组/null约束不兼容")
			}
		case "constant":
			value, err := collector.Resolve(b, nil)
			if err != nil {
				return err
			}
			if _, err = collector.ValidateValue(value, f, false); err != nil {
				return collector.Invalid(loc+".mapping."+key, err.Error())
			}
		case "template":
			var template string
			if json.Unmarshal(b.Value, &template) != nil {
				return collector.Invalid(loc+".mapping."+key, "模板值必须为JSON字符串")
			}
			if (f.Type != "string" && f.Type != "url") || f.Multiple || len(bindingTemplate(b)) > 2048 {
				return collector.Invalid(loc+".mapping."+key, "模板仅支持受限字符串/URL")
			}
			if strings.Contains(edgeToken.ReplaceAllString(bindingTemplate(b), ""), "{{") || strings.Contains(edgeToken.ReplaceAllString(bindingTemplate(b), ""), "}}") {
				return collector.Invalid(loc+".mapping."+key, "模板语法仅允许{{output.key}}/{{input.key}}/{{element}}")
			}
			for _, token := range edgeToken.FindAllStringSubmatch(bindingTemplate(b), -1) {
				src, ok := edgeField(token[1], up, e.Expand)
				if !ok || src.Multiple || src.Type == "json" {
					return collector.Invalid(loc+".mapping."+key, "模板引用必须为已声明标量")
				}
			}
		default:
			return collector.Invalid(loc+".mapping."+key, "映射支持field/constant/template")
		}
	}
	return nil
}

type MappingPreview struct {
	Ordinal    int              `json:"ordinal"`
	ElementKey string           `json:"element_key"`
	Decision   string           `json:"decision"`
	Values     map[string]any   `json:"values,omitempty"`
	Error      *collector.Error `json:"error,omitempty"`
}

func edgeValue(path string, input, output map[string]any, element any) (any, bool) {
	if path == "element" {
		return element, true
	}
	if strings.HasPrefix(path, "output.") {
		v, ok := output[strings.TrimPrefix(path, "output.")]
		return v, ok
	}
	if strings.HasPrefix(path, "input.") {
		v, ok := input[strings.TrimPrefix(path, "input.")]
		return v, ok
	}
	return nil, false
}
func mapEdge(e TaskEdge, down collector.Definition, input, output map[string]any) ([]MappingPreview, error) {
	return mapEdgeMode(e, down, input, output, false)
}
func mapEdgeMode(e TaskEdge, down collector.Definition, input, output map[string]any, partial bool) ([]MappingPreview, error) {
	elements := []any{nil}
	if e.Expand != nil {
		v, ok := edgeValue(e.Expand.Path, input, output, nil)
		if !ok {
			return nil, collector.Invalid("edges."+e.ID+".expand", "缺少展开字段")
		}
		var valid bool
		elements, valid = v.([]any)
		if !valid || len(elements) > 100 {
			return nil, collector.Invalid("edges."+e.ID+".expand", "需要最多100元素的已声明数组")
		}
	}
	rows := []MappingPreview{}
	for i, el := range elements {
		row := MappingPreview{Ordinal: i, ElementKey: fmt.Sprintf("%d:%s", i, collector.Hash(el)), Decision: "ready"}
		if e.Filter != nil {
			v, present := edgeValue(e.Filter.Path, input, output, el)
			var wanted any
			_ = json.Unmarshal(e.Filter.Value, &wanted)
			pass := present && v != nil
			if e.Filter.Op == "equal" {
				pass = present && collector.Hash(v) == collector.Hash(wanted)
			}
			if e.Filter.Op == "not_equal" {
				pass = present && collector.Hash(v) != collector.Hash(wanted)
			}
			if !present && e.Filter.Op != "exists" {
				row.Decision = "invalid"
				row.Error = collector.AsError(collector.Invalid("edges."+e.ID+".filter", "比较字段缺失"))
				rows = append(rows, row)
				continue
			}
			if !pass {
				row.Decision = "filtered"
				rows = append(rows, row)
				continue
			}
		}
		values := map[string]any{}
		var problem error
		for key, b := range e.Mapping {
			var v any
			var present bool
			switch b.Kind {
			case "field":
				v, present = edgeValue(b.Path, input, output, el)
				if !present {
					problem = collector.Invalid("edges."+e.ID+".mapping."+key, "来源字段缺失")
				}
			case "constant":
				v, problem = collector.Resolve(b, nil)
			case "template":
				v = edgeToken.ReplaceAllStringFunc(bindingTemplate(b), func(token string) string {
					path := token[2 : len(token)-2]
					value, ok := edgeValue(path, input, output, el)
					if !ok {
						problem = collector.Invalid("edges."+e.ID+".mapping."+key, "模板来源缺失")
						return ""
					}
					s, err := collector.Scalar(value)
					if err != nil {
						problem = collector.Invalid("edges."+e.ID+".mapping."+key, err.Error())
					}
					return s
				})
			default:
				problem = collector.Invalid("mapping", "映射类型无效")
			}
			if problem != nil {
				break
			}
			values[key] = v
		}
		if problem == nil && partial {
			values, problem = validatePartialInput(down, values)
		} else if problem == nil {
			values, problem = collector.ValidateInput(down, values)
		}
		if problem == nil && !partial {
			_, problem = collector.Prepare(down, values)
		}
		if problem != nil {
			row.Decision = "invalid"
			row.Error = collector.AsError(problem)
		} else {
			row.Values = values
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func TaskMappingPreview(ctx context.Context, owner int64, upID, downID string, edge TaskEdge, input, output map[string]any, modes ...string) (any, error) {
	if len(collector.JSON(input))+len(collector.JSON(output)) > 131072 {
		return nil, collector.Invalid("sample", "样例最多128KiB")
	}
	s := session(ctx)
	defer s.Close()
	_, up, err := collectorVersion(s, owner, upID)
	if err != nil {
		return nil, err
	}
	_, down, err := collectorVersion(s, owner, downID)
	if err != nil {
		return nil, err
	}
	stage := TaskStage{}
	if len(modes) > 0 {
		stage.InputMode = modes[0]
	}
	if stage.InputMode != "" && stage.InputMode != "single" && stage.InputMode != "union" && !gatherStage(stage) {
		return nil, collector.Invalid("input_mode", "无效的输入模式")
	}
	down = gatherRowDefinition(stage, down)
	if err = validateEdge(edge, up, down); err != nil {
		return nil, err
	}
	rows, err := mapEdgeMode(edge, down, input, output, gatherStage(stage))
	return map[string]any{"items": rows, "mapping_hash": collector.Hash(edge), "formal_writes": 0, "fragment_only": gatherStage(stage)}, err
}

func bindingTemplate(b collector.Binding) string {
	var text string
	_ = json.Unmarshal(b.Value, &text)
	return text
}
