// Package dataquery defines the shared query/view/export contract for v2.2.
package dataquery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nekoimi/scrapio/internal/field"
	"github.com/nekoimi/scrapio/internal/output"
)

const MaxRows = 10000
const MaxBytes = 16 * 1024 * 1024

type Filter struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}
type Sort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type Query struct {
	Filters []Filter `json:"filters"`
	Sort    Sort     `json:"sort"`
	Columns []string `json:"columns"`
}

func Decode(raw string) (Query, error) {
	q := Query{}
	if raw == "" {
		return q, nil
	}
	if len(raw) > 8192 {
		return q, errors.New("query exceeds 8 KiB")
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return q, errors.New("query must be a JSON object")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&q); err != nil {
		return q, errors.New("invalid query JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return q, errors.New("query must contain one JSON object")
	}
	return q, nil
}
func Normalize(q Query, schema output.Schema) (Query, error) {
	fields := map[string]output.Field{}
	for _, f := range schema.Fields {
		fields[f.Key] = f
	}
	q.Filters = append([]Filter(nil), q.Filters...)
	if len(q.Filters) > 8 {
		return q, errors.New("at most eight AND filters")
	}
	if q.Filters == nil {
		q.Filters = []Filter{}
	}
	for i, f := range q.Filters {
		def, has := fields[f.Field]
		if !has {
			return q, errors.New("unknown filter field")
		}
		switch f.Op {
		case "empty", "not_empty":
			if len(f.Value) > 0 && string(f.Value) != "null" {
				return q, errors.New("empty filter has no value")
			}
			f.Value = nil
		case "eq", "ne", "contains", "gt", "gte", "lt", "lte":
			if len(f.Value) == 0 || len(f.Value) > 1024 {
				return q, errors.New("filter value requires at most 1 KiB JSON")
			}
			var value any
			d := json.NewDecoder(bytes.NewReader(f.Value))
			d.UseNumber()
			if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF || value == nil {
				return q, errors.New("invalid filter value")
			}
			if def.Multiple || def.Type == "json" {
				return q, errors.New("multi-value/JSON fields currently support empty filters only")
			}
			converted, issue := field.Convert(value, field.Options{Type: def.Type})
			if issue != "" || converted == nil {
				return q, errors.New("filter value does not match field type")
			}
			if def.Type == "integer" || def.Type == "number" {
				converted = fmt.Sprint(converted)
			}
			canonical, _ := json.Marshal(converted)
			f.Value = canonical
			if f.Op == "contains" && def.Type != "string" {
				return q, errors.New("contains requires scalar text")
			}
			if f.Op != "eq" && f.Op != "ne" && f.Op != "contains" && def.Type != "integer" && def.Type != "number" && def.Type != "date" && def.Type != "datetime" {
				return q, errors.New("range requires numeric or date field")
			}
		default:
			return q, errors.New("unsupported filter operator")
		}
		q.Filters[i] = f
	}
	if q.Sort.Field == "" {
		q.Sort.Field = "@first_observed_at"
	}
	if q.Sort.Direction == "" {
		q.Sort.Direction = "desc"
	}
	if q.Sort.Direction != "asc" && q.Sort.Direction != "desc" {
		return q, errors.New("sort direction must be asc or desc")
	}
	switch q.Sort.Field {
	case "@first_observed_at", "@last_observed_at", "@revision":
	default:
		f, has := fields[q.Sort.Field]
		if !has || f.Multiple || f.Type == "json" {
			return q, errors.New("unsupported sort field")
		}
	}
	if len(q.Columns) == 0 {
		for _, f := range schema.Fields {
			q.Columns = append(q.Columns, f.Key)
		}
	}
	if len(q.Columns) > 30 {
		return q, errors.New("at most thirty columns")
	}
	seen := map[string]bool{}
	for _, key := range q.Columns {
		if _, has := fields[key]; !has || seen[key] {
			return q, errors.New("columns must be distinct known field keys")
		}
		seen[key] = true
	}
	return q, nil
}

// SQL interpolates only operator/type allowlists; every field key and value is a bound parameter.
func SQL(q Query, schema output.Schema) (string, []any, string, []any) {
	fields := map[string]output.Field{}
	for _, f := range schema.Fields {
		fields[f.Key] = f
	}
	clauses := []string{}
	args := []any{}
	for _, f := range q.Filters {
		operand := string(f.Value)
		if def := fields[f.Field]; len(f.Value) > 0 && (def.Type == "integer" || def.Type == "number") {
			var v any
			decoder := json.NewDecoder(bytes.NewReader(f.Value))
			decoder.UseNumber()
			_ = decoder.Decode(&v)
			converted, _ := field.Convert(v, field.Options{Type: def.Type})
			raw, _ := json.Marshal(converted)
			operand = string(raw)
		}
		value := "(r.record_values -> ?::text)"
		text := "(r.record_values ->> ?::text)"
		switch f.Op {
		case "empty", "not_empty":
			condition := "(" + value + " IS NULL OR " + value + " IN ('null'::jsonb,'\"\"'::jsonb,'[]'::jsonb))"
			args = append(args, f.Field, f.Field)
			if f.Op == "not_empty" {
				condition = "NOT " + condition
			}
			clauses = append(clauses, condition)
		case "eq", "ne":
			operator := "="
			if f.Op == "ne" {
				operator = "<>"
			}
			clauses = append(clauses, value+operator+"?::jsonb")
			args = append(args, f.Field, operand)
		case "contains":
			var needle string
			_ = json.Unmarshal(f.Value, &needle)
			clauses = append(clauses, "strpos("+text+",?)>0")
			args = append(args, f.Field, needle)
		default:
			operators := map[string]string{"gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
			def := fields[f.Field]
			if def.Type == "integer" || def.Type == "number" {
				clauses = append(clauses, "(CASE WHEN jsonb_typeof("+value+")='number' THEN "+text+"::numeric END)"+operators[f.Op]+"?::numeric")
				args = append(args, f.Field, f.Field, operand)
			} else {
				var date string
				_ = json.Unmarshal(f.Value, &date)
				clauses = append(clauses, text+operators[f.Op]+"?")
				args = append(args, f.Field, date)
			}
		}
	}
	where := ""
	if len(clauses) > 0 {
		where = " AND " + strings.Join(clauses, " AND ")
	}
	direction := strings.ToUpper(q.Sort.Direction)
	orderArgs := []any{}
	order := "r." + strings.TrimPrefix(q.Sort.Field, "@")
	if def, has := fields[q.Sort.Field]; has {
		order = "(r.record_values ->> ?::text) COLLATE \"C\""
		orderArgs = append(orderArgs, q.Sort.Field)
		if def.Type == "number" || def.Type == "integer" {
			order = "(CASE WHEN jsonb_typeof(r.record_values -> ?::text)='number' THEN (r.record_values ->> ?::text)::numeric END)"
			orderArgs = append(orderArgs, q.Sort.Field)
		}
	}
	return where, args, fmt.Sprintf("%s %s NULLS LAST,r.id %s", order, direction, direction), orderArgs
}
