package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"github.com/antchfx/xpath"
)

// ExecutableNodeTypes is the single allowlist for publication and execution.
// Extend it only together with the worker's dispatch and contract tests.
var ExecutableNodeTypes = map[string]struct{}{
	"extract": {}, "discover": {}, "transform": {}, "validate": {}, "script": {},
}

var supportedJSONPath = regexp.MustCompile(`^\$(?:\.[A-Za-z_][A-Za-z0-9_-]*|\[[0-9]+\])*$`)

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("must be an absolute http(s) URL")
	}
	return nil
}

func validateFetchOptions(options FetchOptions) error {
	if options.Mode != "" && options.Mode != "http" && options.Mode != "browser" {
		return fmt.Errorf("mode: only http or browser is supported")
	}
	if options.TimeoutMS < 0 || options.TimeoutMS > 120000 {
		return fmt.Errorf("timeout_ms: must be between 0 and 120000")
	}
	if len(options.Actions) > 0 && options.Mode != "browser" {
		return fmt.Errorf("actions: browser mode is required")
	}
	if len(options.Actions) > 32 {
		return fmt.Errorf("actions: at most 32 actions are supported")
	}
	for i, action := range options.Actions {
		path := fmt.Sprintf("actions[%d]", i)
		if action.RunOn != "" && action.RunOn != "trigger" && action.RunOn != "list" && action.RunOn != "detail" {
			return fmt.Errorf("%s.run_on: only trigger, list or detail is supported", path)
		}
		if action.TimeoutMS < 0 || action.TimeoutMS > 60000 {
			return fmt.Errorf("%s.timeout_ms: must be between 0 and 60000", path)
		}
		switch action.Type {
		case "navigate":
			if err := validateHTTPURL(action.Value); err != nil {
				return fmt.Errorf("%s.value: %w", path, err)
			}
		case "wait", "click", "input":
			if strings.TrimSpace(action.Selector) == "" {
				return fmt.Errorf("%s.selector: required", path)
			}
		case "scroll":
			pixels, err := strconv.Atoi(action.Value)
			if err != nil || pixels < -100000 || pixels > 100000 {
				return fmt.Errorf("%s.value: expected pixels between -100000 and 100000", path)
			}
		default:
			return fmt.Errorf("%s.type: unsupported browser action", path)
		}
	}
	return nil
}

func validateExecutableNode(node Node) error {
	allowed := map[string]map[string]bool{
		"extract":   {"fields": true, "content_type": true, "run_on": true, "items_path": true},
		"discover":  {"fields": true, "content_type": true, "run_on": true, "url_field": true},
		"transform": {"operations": true, "run_on": true},
		"validate":  {"fields": true, "run_on": true},
		"script":    {"script": true, "timeout_ms": true, "run_on": true, "input_fields": true, "output_fields": true},
	}
	if _, ok := ExecutableNodeTypes[node.Type]; !ok {
		return fmt.Errorf("type: unsupported executor")
	}
	for key := range node.Config {
		if !allowed[node.Type][key] {
			return fmt.Errorf("config.%s: not executed", key)
		}
	}
	if raw, ok := node.Config["run_on"]; ok {
		steps := []any{raw}
		if list, ok := raw.([]any); ok {
			steps = list
		}
		for _, step := range steps {
			name, ok := step.(string)
			if !ok || (name != "trigger" && name != "detail") {
				return fmt.Errorf("config.run_on: only trigger and detail are supported")
			}
			if node.Type == "discover" && name != "trigger" {
				return fmt.Errorf("config.run_on: discover only runs on trigger")
			}
		}
	}
	if raw, ok := node.Config["content_type"]; ok && raw != "html" && raw != "json" {
		return fmt.Errorf("config.content_type: only html or json is supported")
	}
	if raw, ok := node.Config["items_path"]; ok {
		path, ok := raw.(string)
		if !ok || !supportedJSONPath.MatchString(path) || node.Config["content_type"] != "json" {
			return fmt.Errorf("config.items_path: JSON content and supported JSONPath required")
		}
	}
	switch node.Type {
	case "extract", "discover":
		raw, ok := node.Config["fields"].([]any)
		if !ok || len(raw) == 0 {
			return fmt.Errorf("config.fields: at least one field is required")
		}
		encoded, _ := json.Marshal(raw)
		var fields []FieldRule
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return fmt.Errorf("config.fields: %w", err)
		}
		seen := map[string]bool{}
		for i, field := range fields {
			path := fmt.Sprintf("config.fields[%d]", i)
			fieldConfig, ok := raw[i].(map[string]any)
			if !ok {
				return fmt.Errorf("%s: must be an object", path)
			}
			for key := range fieldConfig {
				if !map[string]bool{"name": true, "selector": true, "selector_type": true, "attribute": true, "regex": true, "clean": true, "type": true, "required": true, "multiple": true, "default": true}[key] {
					return fmt.Errorf("%s.%s: not executed", path, key)
				}
			}
			if field.Name == "" || field.Selector == "" {
				return fmt.Errorf("%s: name and selector are required", path)
			}
			if seen[field.Name] {
				return fmt.Errorf("%s.name: duplicate field", path)
			}
			seen[field.Name] = true
			selectorType := strings.ToLower(field.SelectorType)
			if selectorType == "" {
				if strings.HasPrefix(field.Selector, "$") {
					selectorType = "jsonpath"
				} else {
					selectorType = "css"
				}
			}
			switch selectorType {
			case "css":
				if _, err := cascadia.Parse(field.Selector); err != nil {
					return fmt.Errorf("%s.selector: %w", path, err)
				}
			case "xpath":
				if _, err := xpath.Compile(field.Selector); err != nil {
					return fmt.Errorf("%s.selector: %w", path, err)
				}
			case "jsonpath", "json_path":
				if !supportedJSONPath.MatchString(field.Selector) {
					return fmt.Errorf("%s.selector: unsupported JSONPath subset", path)
				}
			default:
				return fmt.Errorf("%s.selector_type: unsupported selector type", path)
			}
			contentType, _ := node.Config["content_type"].(string)
			if contentType == "json" && selectorType != "jsonpath" && selectorType != "json_path" {
				return fmt.Errorf("%s.selector_type: JSON content requires JSONPath", path)
			}
			if contentType == "html" && (selectorType == "jsonpath" || selectorType == "json_path") {
				return fmt.Errorf("%s.selector_type: HTML content cannot use JSONPath", path)
			}
			if field.Regex != "" {
				if _, err := regexp.Compile(field.Regex); err != nil {
					return fmt.Errorf("%s.regex: %w", path, err)
				}
			}
			switch strings.ToLower(field.Type) {
			case "", "string", "int", "integer", "float", "number", "bool", "boolean", "array":
			default:
				return fmt.Errorf("%s.type: unsupported type", path)
			}
			if field.Multiple && field.Type != "" && field.Type != "array" {
				return fmt.Errorf("%s.type: multiple values require array type", path)
			}
			if field.Clean != "" && field.Clean != "whitespace" {
				return fmt.Errorf("%s.clean: unsupported clean operation", path)
			}
		}
		if node.Type == "discover" {
			if raw, exists := node.Config["url_field"]; exists {
				name, ok := raw.(string)
				if !ok || !seen[name] {
					return fmt.Errorf("config.url_field: must name an extracted field")
				}
			} else if len(fields) != 1 {
				return fmt.Errorf("config.url_field: required when multiple fields are extracted")
			}
		}
	case "transform":
		operations, ok := node.Config["operations"].([]any)
		if !ok || len(operations) == 0 {
			return fmt.Errorf("config.operations: at least one operation is required")
		}
		for i, raw := range operations {
			op, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("config.operations[%d]: must be an object", i)
			}
			for key := range op {
				if !map[string]bool{"op": true, "field": true, "to": true, "separator": true, "value": true}[key] {
					return fmt.Errorf("config.operations[%d].%s: not executed", i, key)
				}
			}
			name, _ := op["op"].(string)
			field, _ := op["field"].(string)
			if field == "" {
				return fmt.Errorf("config.operations[%d].field: required", i)
			}
			switch name {
			case "rename":
				if to, _ := op["to"].(string); to == "" {
					return fmt.Errorf("config.operations[%d].to: required", i)
				}
			case "trim", "lower", "upper", "split", "join", "default", "set", "delete":
			default:
				return fmt.Errorf("config.operations[%d].op: unsupported operation", i)
			}
		}
	case "validate":
		fields, ok := node.Config["fields"].([]any)
		if !ok || len(fields) == 0 {
			return fmt.Errorf("config.fields: at least one validation is required")
		}
		for i, raw := range fields {
			field, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("config.fields[%d]: must be an object", i)
			}
			for key := range field {
				if !map[string]bool{"name": true, "required": true, "type": true, "regex": true, "enum": true}[key] {
					return fmt.Errorf("config.fields[%d].%s: not executed", i, key)
				}
			}
			if name, _ := field["name"].(string); name == "" {
				return fmt.Errorf("config.fields[%d].name: required", i)
			}
			if pattern, _ := field["regex"].(string); pattern != "" {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("config.fields[%d].regex: %w", i, err)
				}
			}
		}
	case "script":
		if value, ok := node.Config["timeout_ms"]; ok {
			timeout, ok := value.(float64)
			if !ok || timeout <= 0 || timeout > 2000 {
				return fmt.Errorf("config.timeout_ms: must be between 1 and 2000")
			}
		}
	}
	return nil
}
