package co

import (
	"encoding/json"
	"fmt"
	"github.com/nekoimi/scrapio/internal/collector"
	"strings"
)

// TaskDefinition belongs to the orchestration domain, never to a collector.
type TaskDefinition struct {
	DefinitionVersion string       `json:"definition_version"`
	EntryStage        string       `json:"entry_stage"`
	Source            ManualSource `json:"source"`
	Stages            []TaskStage  `json:"stages"`
	Edges             []TaskEdge   `json:"edges"`
	Budget            TaskBudget   `json:"budget"`
	OverlapPolicy     string       `json:"overlap_policy"`
}
type ManualSource struct {
	Kind   string           `json:"kind"`
	Inputs []map[string]any `json:"inputs"`
}
type TaskStage struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	CollectorVersionID string      `json:"collector_version_id"`
	Save               SaveBinding `json:"save"`
	OnError            string      `json:"on_error"`
}
type SaveBinding struct {
	TableID       string                       `json:"table_id"`
	SchemaVersion int                          `json:"schema_version"`
	SchemaHash    string                       `json:"schema_hash"`
	Mapping       map[string]collector.Binding `json:"mapping"`
	UniqueKey     []string                     `json:"unique_key"`
	UpdatePolicy  string                       `json:"update_policy"`
	NullPolicy    string                       `json:"null_policy"`
}
type TaskBudget struct {
	MaxDispatches int `json:"max_dispatches,omitempty"`
	MaxWorkItems  int `json:"max_work_items"`
	MaxOutputs    int `json:"max_outputs"`
	MaxPending    int `json:"max_pending"`
	BrowserSlots  int `json:"browser_slots"`
	MaxDurationMS int `json:"max_duration_ms"`
}
type TableSchema struct {
	Fields    []collector.Field `json:"fields"`
	UniqueKey []string          `json:"unique_key"`
}
type TaskManifest struct {
	Definition          TaskDefinition         `json:"definition"`
	Collector           FrozenCollector        `json:"collector"`
	CollectorDefinition collector.Definition   `json:"collector_definition"`
	Schema              TableSchema            `json:"schema"`
	Hash                string                 `json:"hash"`
	Stages              map[string]FrozenStage `json:"stage_manifests,omitempty"`
	Trial               *TrialOptions          `json:"trial,omitempty"`
}
type TrialOptions struct {
	Mode      string                          `json:"mode"`
	Documents map[string][]collector.Document `json:"documents,omitempty"`
}

type FrozenStage struct {
	Collector  FrozenCollector      `json:"collector"`
	Definition collector.Definition `json:"definition"`
	Schema     TableSchema          `json:"schema"`
}

// ForStage keeps O2 manifests readable while selecting a frozen O3 executor.
func (m TaskManifest) ForStage(id string) TaskManifest {
	if f, ok := m.Stages[id]; ok {
		m.Collector, m.CollectorDefinition, m.Schema = f.Collector, f.Definition, f.Schema
	}
	return m
}
func (m TaskManifest) Stage(id string) TaskStage {
	for _, s := range m.Definition.Stages {
		if s.ID == id {
			return s
		}
	}
	return TaskStage{}
}

type FrozenCollector struct {
	ID                 string          `json:"id"`
	CollectorID        string          `json:"collector_id"`
	Number             int             `json:"number"`
	DefinitionHash     string          `json:"definition_hash"`
	InputSchemaHash    string          `json:"input_schema_hash"`
	OutputSchemaHash   string          `json:"output_schema_hash"`
	InterpreterVersion string          `json:"interpreter_version"`
	Requirements       json.RawMessage `json:"requirements"`
}

func freezeCollector(v Version) FrozenCollector {
	return FrozenCollector{ID: v.ID, CollectorID: v.CollectorID, Number: v.Number, DefinitionHash: v.DefinitionHash, InputSchemaHash: v.InputSchemaHash, OutputSchemaHash: v.OutputSchemaHash, InterpreterVersion: v.InterpreterVersion, Requirements: json.RawMessage(v.Requirements)}
}

func (d TaskDefinition) Validate() error {
	if e := d.validateGraph(); e != nil {
		return e
	}
	if d.Source.Kind != "manual" || len(d.Source.Inputs) < 1 || len(d.Source.Inputs) > 100 || len(collector.JSON(d.Source.Inputs)) > 512*1024 {
		return collector.Invalid("source", "仅支持1..100条手动输入，每批最多512KiB")
	}
	if d.OverlapPolicy != "skip" {
		return collector.Invalid("policy", "需要skip及continue/pause_stage策略")
	}
	b := d.Budget
	if b.MaxDispatches < 0 || b.MaxDispatches > 10000 || b.MaxWorkItems < 1 || b.MaxWorkItems > 10000 || b.MaxWorkItems < len(d.Source.Inputs) || b.MaxOutputs < 1 || b.MaxOutputs > 100000 || b.MaxPending < 1 || b.MaxPending > 10000 || b.BrowserSlots < 1 || b.BrowserSlots > 8 || b.MaxDurationMS < 1000 || b.MaxDurationMS > 86400000 {
		return collector.Invalid("budget", "工作项/输出/队列/浏览器槽位或时长预算无效")
	}
	return nil
}
func (s TableSchema) Validate() error {
	return collector.ValidateSchema(s.Fields, s.UniqueKey)
}
func validateBinding(b SaveBinding, s TableSchema, d collector.Definition) error {
	if e := s.Validate(); e != nil {
		return e
	}
	if b.TableID == "" || b.SchemaVersion < 1 || b.SchemaHash != collector.Hash(s) || collector.Hash(b.UniqueKey) != collector.Hash(s.UniqueKey) {
		return collector.Fault("SCHEMA_CONFLICT", "save", "数据表Schema或唯一键已变化，请重新预演绑定")
	}
	if (b.UpdatePolicy != "merge_present" && b.UpdatePolicy != "replace_present") || (b.NullPolicy != "keep" && b.NullPolicy != "clear") {
		return collector.Invalid("save", "更新或null策略无效")
	}
	fields := map[string]collector.Field{}
	outputs := map[string]collector.Field{}
	for _, f := range s.Fields {
		fields[f.Key] = f
		if f.Required {
			if _, ok := b.Mapping[f.Key]; !ok {
				return collector.Invalid("save.mapping."+f.Key, "必填表字段缺少映射")
			}
		}
	}
	for _, f := range d.Output.Fields {
		outputs[f.Key] = f
	}
	for k, v := range b.Mapping {
		target, ok := fields[k]
		if !ok {
			return collector.Invalid("save.mapping."+k, "未知表字段")
		}
		switch v.Kind {
		case "field":
			f, ok := outputs[strings.TrimPrefix(v.Path, "output.")]
			if !strings.HasPrefix(v.Path, "output.") || !ok || f.Type != target.Type || f.Multiple != target.Multiple || (!target.Nullable && f.Nullable) || (target.Required && !f.Required) {
				return collector.Invalid("save.mapping."+k, "输出字段不存在或类型/数组/null约束不兼容")
			}
		case "constant":
			value, e := collector.Resolve(v, map[string]any{})
			if e != nil {
				return e
			}
			if _, e = collector.ValidateValue(value, target, false); e != nil {
				return collector.Invalid("save.mapping."+k, e.Error())
			}
		default:
			return collector.Invalid("save.mapping."+k, "保存映射支持输出字段或常量")
		}
	}
	return nil
}

type InputPreview struct {
	Ordinal   int              `json:"ordinal"`
	Values    map[string]any   `json:"values,omitempty"`
	Error     *collector.Error `json:"error,omitempty"`
	SourceKey string           `json:"source_key"`
}

func PreviewInputs(d collector.Definition, inputs []map[string]any) ([]InputPreview, error) {
	if len(inputs) < 1 || len(inputs) > 100 {
		return nil, collector.Invalid("inputs", "需要1..100条结构化输入")
	}
	rows := make([]InputPreview, 0, len(inputs))
	for i, input := range inputs {
		row := InputPreview{Ordinal: i, SourceKey: fmt.Sprintf("root:%d:%s", i, collector.Hash(input))}
		v, e := collector.ValidateInput(d, input)
		if e == nil {
			_, e = collector.Prepare(d, v)
		}
		if e != nil {
			row.Error = collector.AsError(e)
		} else {
			row.Values = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func mapValues(b SaveBinding, s TableSchema, output map[string]any) (map[string]any, string, error) {
	values := map[string]any{}
	for _, f := range s.Fields {
		binding, exists := b.Mapping[f.Key]
		if !exists {
			if f.Required {
				return nil, "", collector.Invalid("save."+f.Key, "必填字段缺少映射")
			}
			continue
		}
		var v any
		var e error
		present := true
		if binding.Kind == "field" {
			v, present = output[strings.TrimPrefix(binding.Path, "output.")]
		} else {
			v, e = collector.Resolve(binding, map[string]any{})
		}
		if e != nil {
			return nil, "", e
		}
		if !present {
			if f.Required {
				return nil, "", collector.Invalid("save."+f.Key, "缺少必填输出")
			}
			continue
		}
		v, e = collector.ValidateValue(v, f, false)
		if e != nil {
			return nil, "", collector.Invalid("save."+f.Key, e.Error())
		}
		values[f.Key] = v
	}
	key := []any{}
	for _, k := range b.UniqueKey {
		v, ok := values[k]
		if !ok || v == nil {
			return nil, "", collector.Invalid("save."+k, "唯一键缺失或为null")
		}
		key = append(key, v)
	}
	return values, collector.Hash(key), nil
}
func effectiveValues(old, incoming map[string]any, b SaveBinding) map[string]any {
	out := map[string]any{}
	if b.UpdatePolicy == "merge_present" {
		for k, v := range old {
			out[k] = v
		}
	}
	for k, v := range incoming {
		if v == nil && b.NullPolicy == "keep" {
			if oldValue, ok := old[k]; ok {
				out[k] = oldValue
			} else {
				out[k] = nil
			}
			continue
		}
		out[k] = v
	}
	return out
}
