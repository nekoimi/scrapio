package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/nekoimi/scrapio/internal/field"
	"regexp"
	"strconv"
	"strings"
)

type JSONField struct {
	field.Options
	Name    string `json:"name"`
	Pointer string `json:"pointer"`
}
type JSONPlan struct {
	ArrayPointer string      `json:"array_pointer"`
	MaxRecords   int         `json:"max_records"`
	Fields       []JSONField `json:"fields"`
}

func DecodePlan(data []byte) (JSONPlan, error) {
	var plan JSONPlan
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || len(raw["array_pointer"]) == 0 || raw["array_pointer"][0] != '"' {
		return plan, errors.New("array_pointer must be an explicit JSON pointer string")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, errors.New("unsupported JSON record configuration")
	}
	var fields []map[string]json.RawMessage
	if json.Unmarshal(raw["fields"], &fields) != nil {
		return plan, errors.New("fields must be an array")
	}
	for _, field := range fields {
		if len(field["pointer"]) == 0 || field["pointer"][0] != '"' {
			return plan, errors.New("each field requires an explicit pointer string")
		}
	}
	return plan, plan.Validate()
}

func (p JSONPlan) Validate() error {
	if !validPointer(p.ArrayPointer) || p.MaxRecords < 1 || p.MaxRecords > 20 || p.Fields == nil || len(p.Fields) > 30 {
		return errors.New("array pointer, max_records (1..20) and bounded fields required")
	}
	seen := map[string]bool{}
	keys := map[string]bool{}
	for _, field := range p.Fields {
		if err := field.Options.Validate(); err != nil {
			return err
		}
		if !regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_-]{0,63}$`).MatchString(field.Name) || seen[field.Name] || !validPointer(field.Pointer) {
			return errors.New("unique field names and JSON pointers required")
		}
		seen[field.Name] = true
		key := field.Key
		if key == "" {
			key = field.Name
		}
		if keys[key] {
			return errors.New("field_key must be unique")
		}
		keys[key] = true
	}
	return nil
}
func validPointer(pointer string) bool {
	if len(pointer) > 2048 || pointer != "" && !strings.HasPrefix(pointer, "/") {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' {
			if i+1 >= len(pointer) || pointer[i+1] != '0' && pointer[i+1] != '1' {
				return false
			}
			i++
		}
	}
	return true
}
func Resolve(value any, pointer string) (any, bool) {
	if !validPointer(pointer) {
		return nil, false
	}
	if pointer == "" {
		return value, true
	}
	for _, raw := range strings.Split(pointer[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[key]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(v) || strconv.Itoa(index) != key {
				return nil, false
			}
			value = v[index]
		default:
			return nil, false
		}
	}
	return value, true
}
func InspectJSON(content string, plan JSONPlan) (map[string]any, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	value, err := DecodeJSON(content)
	if err != nil {
		return nil, err
	}
	selected, ok := Resolve(value, plan.ArrayPointer)
	if !ok {
		return nil, errors.New("array pointer matched no value")
	}
	array, ok := selected.([]any)
	if !ok {
		return nil, errors.New("array pointer must select an array")
	}
	records := make([]any, 0)
	for index, record := range array {
		if index >= plan.MaxRecords {
			break
		}
		fields := make([]any, 0)
		for _, field := range plan.Fields {
			item, exists := Resolve(record, field.Pointer)
			raw := ""
			if exists {
				data, _ := json.Marshal(item)
				raw = string(data)
				if len(raw) > 2048 {
					raw = raw[:2048] + "…"
				}
			}
			fields = append(fields, map[string]any{"name": field.Name, "pointer": field.Pointer, "found": exists, "raw_value": raw})
		}
		records = append(records, map[string]any{"index": index, "fields": fields})
	}
	return map[string]any{"records": records, "match_count": len(array), "truncated": len(array) > plan.MaxRecords, "network_accessed": false, "dry_run": true}, nil
}
