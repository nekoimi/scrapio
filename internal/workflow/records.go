package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/nekoimi/scrapio/internal/record"
)

// RecordCandidates is shared by template execution and future dry-run previews.
// All candidates are extracted and validated before any database write occurs.
func RecordCandidates(d Definition, document FetchResult, role string, schema record.Schema) ([]map[string]any, error) {
	values, _, err := TraceRecordCandidates(d, document, role, schema)
	if err != nil {
		return nil, err
	}
	return values, err
}

type RecordStep struct {
	Candidate int               `json:"candidate"`
	Node      string            `json:"node"`
	Type      string            `json:"type"`
	Values    map[string]any    `json:"values"`
	Fields    []FieldDiagnostic `json:"fields,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// TraceRecordCandidates is the execution path used by both the worker and dry-run.
func TraceRecordCandidates(d Definition, document FetchResult, role string, schema record.Schema) ([]map[string]any, []RecordStep, error) {
	if err := d.ValidateRecordSchema(schema, role); err != nil {
		return nil, nil, err
	}
	contents := []FetchResult{document}
	first := d.Nodes[0]
	if path, ok := first.Config["items_path"].(string); ok {
		var root any
		if err := json.Unmarshal([]byte(document.JSON), &root); err != nil {
			return nil, nil, fmt.Errorf("nodes[0].items_path: invalid JSON: %w", err)
		}
		value, err := jsonPathValue(root, path)
		if err != nil {
			return nil, nil, err
		}
		items, ok := value.([]any)
		if !ok {
			return nil, nil, fmt.Errorf("nodes[0].items_path: expected an array")
		}
		if len(items) == 0 || len(items) > 1000 {
			return nil, nil, fmt.Errorf("nodes[0].items_path: require 1–1000 items")
		}
		contents = make([]FetchResult, 0, len(items))
		for i, item := range items {
			if _, ok := item.(map[string]any); !ok {
				return nil, nil, fmt.Errorf("items[%d]: expected object", i)
			}
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, nil, err
			}
			contents = append(contents, FetchResult{JSON: string(encoded)})
		}
	}
	candidates := make([]map[string]any, 0, len(contents))
	steps := make([]RecordStep, 0, len(contents)*len(d.Nodes))
	for i, content := range contents {
		values := map[string]any{}
		for nodeIndex, node := range d.Nodes {
			if !nodeApplies(node, role) {
				continue
			}
			var err error
			step := RecordStep{Candidate: i, Node: node.Name, Type: node.Type}
			switch node.Type {
			case "extract":
				step.Fields = DiagnoseExtractNode(node, nodeIndex, content)
				values, err = extractNodeValues(node, content)
			case "transform":
				err = ApplyTransform(values, node.Config)
			case "validate":
				err = ValidateValues(values, node.Config)
			}
			if err != nil {
				step.Error = err.Error()
				step.Values = values
				steps = append(steps, step)
				return candidates, steps, fmt.Errorf("candidate[%d].%s: %w", i, node.Name, err)
			}
			snapshot := make(map[string]any, len(values))
			for key, value := range values {
				snapshot[key] = value
			}
			step.Values = snapshot
			steps = append(steps, step)
		}
		prepared, err := record.Prepare(schema, values)
		if err != nil {
			steps = append(steps, RecordStep{Candidate: i, Node: "dataset_schema", Type: "validate", Values: values, Error: err.Error()})
			return candidates, steps, fmt.Errorf("candidate[%d]: %w", i, err)
		}
		candidates = append(candidates, prepared.Values)
	}
	return candidates, steps, nil
}

// ValidateRecordSchema checks the statically known output names at publication.
func (d Definition) ValidateRecordSchema(schema record.Schema, role string) error {
	if err := d.ValidateExecutable(); err != nil {
		return err
	}
	if d.Persistence != "records" {
		return fmt.Errorf("persistence: records required")
	}
	names := map[string]bool{}
	for _, node := range d.Nodes {
		if !nodeApplies(node, role) {
			continue
		}
		if node.Type == "extract" {
			fields, err := fieldRules(node.Config["fields"])
			if err != nil {
				return err
			}
			for _, f := range fields {
				names[f.Name] = true
			}
		}
		if node.Type == "transform" {
			for _, raw := range node.Config["operations"].([]any) {
				op := raw.(map[string]any)
				name := stringValue(op["field"])
				switch op["op"] {
				case "rename":
					if names[name] {
						delete(names, name)
						names[stringValue(op["to"])] = true
					}
				case "delete":
					delete(names, name)
				case "set", "default":
					names[name] = true
				}
			}
		}
	}
	fields := map[string]bool{}
	for _, field := range schema.Fields {
		fields[field.Key] = true
		if field.Required && !names[field.Key] {
			return fmt.Errorf("dataset.fields.%s: required output missing", field.Key)
		}
	}
	for name := range names {
		if !fields[name] {
			return fmt.Errorf("dataset.fields.%s: unknown output field", name)
		}
	}
	for _, key := range schema.UniqueKeyFields {
		if !names[key] {
			return fmt.Errorf("dataset.unique_key_fields.%s: output missing", key)
		}
	}
	return nil
}
