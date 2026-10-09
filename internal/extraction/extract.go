// Package extraction is the v2.2 saved-document interpreter. Preview adapters
// only attach revision/input metadata; trial and run adapters reuse Extract.
package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/field"
)

const InterpreterVersion = "extract.v1"

type Input struct {
	Content string
	Format  string
	URL     string
	BaseURL string
	Stage   string
}
type FieldResult struct {
	Name       string   `json:"name"`
	Key        string   `json:"field_key"`
	MatchCount int      `json:"match_count"`
	RawValues  []any    `json:"raw_values"`
	RawJSON    string   `json:"raw_json"`
	ValueJSON  string   `json:"value_json"`
	Value      any      `json:"value"`
	Errors     []string `json:"errors"`
	Valid      bool     `json:"valid"`
	Truncated  bool     `json:"truncated"`
	Pointer    string   `json:"pointer,omitempty"`
	Locator    any      `json:"locator,omitempty"`
}
type Record struct {
	Index  int            `json:"index"`
	Fields []FieldResult  `json:"fields"`
	Values map[string]any `json:"values"`
	Valid  bool           `json:"valid"`
}
type Result struct {
	sizeBytes            int
	Interpreter          string   `json:"interpreter_version"`
	Stage                string   `json:"stage"`
	StepID               string   `json:"step_id"`
	MatchCount           int      `json:"match_count"`
	MatchCountLowerBound bool     `json:"match_count_lower_bound"`
	Truncated            bool     `json:"truncated"`
	ValidCount           int      `json:"valid_count"`
	InvalidCount         int      `json:"invalid_count"`
	Records              []Record `json:"records"`
	Warnings             []string `json:"warnings"`
	NetworkAccessed      bool     `json:"network_accessed"`
	DryRun               bool     `json:"dry_run"`
}

func Extract(ctx context.Context, input Input, plan Plan) (Result, error) {
	result := Result{Interpreter: InterpreterVersion, Stage: input.Stage, StepID: plan.StepID, Records: []Record{}, Warnings: []string{}, DryRun: true}
	if len(input.Content) > capture.MaxBytes || input.Content == "" || input.Stage != "list" && input.Stage != "detail" {
		return result, errors.New("bounded saved input and list/detail stage required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	switch plan.Kind {
	case "json_records":
		if input.Format != "json" || input.Stage != "list" {
			return result, errors.New("JSON records require a JSON list snapshot")
		}
		if err := plan.JSON.Validate(); err != nil {
			return result, err
		}
		value, err := capture.DecodeJSON(input.Content)
		if err != nil {
			return result, err
		}
		selected, found := capture.Resolve(value, plan.JSON.ArrayPointer)
		if !found {
			return result, errors.New("record array pointer matched no value")
		}
		records, ok := selected.([]any)
		if !ok {
			return result, errors.New("record pointer must select an array")
		}
		result.MatchCount = len(records)
		result.Truncated = len(records) > plan.JSON.MaxRecords
		for index, raw := range records {
			if index >= plan.JSON.MaxRecords {
				break
			}
			if err = ctx.Err(); err != nil {
				return result, err
			}
			record := newRecord(index)
			for _, rule := range plan.JSON.Fields {
				value, found := capture.Resolve(raw, rule.Pointer)
				values := []any{}
				if found {
					if array, ok := value.([]any); rule.Multiple && ok {
						values = array
					} else {
						values = append(values, value)
					}
				}
				f := process(rule.Name, rule.Options, values, false)
				f.Pointer = rule.Pointer
				addField(&record, f)
			}
			if err := addRecord(&result, record); err != nil {
				return result, err
			}
		}
	case "record_set":
		if input.Format != "html" {
			return result, errors.New("HTML records require an HTML snapshot")
		}
		if err := plan.HTML.Validate(); err != nil {
			return result, err
		}
		if err := extractHTML(ctx, input, plan, &result); err != nil {
			return result, err
		}
	default:
		return result, errors.New("unsupported extraction step")
	}
	if result.MatchCount == 0 {
		result.Warnings = append(result.Warnings, "NO_RECORDS")
	}
	if result.Truncated {
		result.Warnings = append(result.Warnings, "RECORD_BUDGET_REACHED")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 2*capture.MaxBytes {
		return result, errors.New("preview output exceeds 2 MiB budget")
	}
	return result, nil
}
func newRecord(index int) Record {
	return Record{Index: index, Fields: []FieldResult{}, Values: map[string]any{}, Valid: true}
}
func addField(record *Record, f FieldResult) {
	seen := map[string]bool{}
	codes := []string{}
	for _, code := range f.Errors {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	f.Errors = codes
	record.Fields = append(record.Fields, f)
	record.Values[f.Name] = f.Value
	record.Valid = record.Valid && f.Valid
}
func addRecord(result *Result, record Record) error {
	if len(record.Fields) == 0 {
		record.Valid = false
		result.Warnings = append(result.Warnings, "NO_FIELDS")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	result.sizeBytes += len(encoded)
	if result.sizeBytes > 2*capture.MaxBytes {
		return errors.New("preview output exceeds 2 MiB budget")
	}
	result.Records = append(result.Records, record)
	if record.Valid {
		result.ValidCount++
	} else {
		result.InvalidCount++
	}
	return nil
}
func process(name string, options field.Options, raw []any, truncated bool) FieldResult {
	return processPrepared(name, options, raw, raw, truncated)
}

func processPrepared(name string, options field.Options, raw, prepared []any, truncated bool) FieldResult {
	key := options.Key
	if key == "" {
		key = name
	}
	f := FieldResult{Name: name, Key: key, MatchCount: len(raw), RawValues: []any{}, Errors: []string{}, Truncated: truncated}
	values := make([]any, 0, len(raw))
	budget := 0
	outputBudget := 0
	for index, value := range raw {
		encoded, _ := json.Marshal(value)
		budget += len(encoded)
		if index >= 50 || budget > 8192 {
			f.Truncated = true
			f.Errors = append(f.Errors, "VALUE_BUDGET_EXCEEDED")
			break
		}
		f.RawValues = append(f.RawValues, value)
		processed, code := field.Convert(prepared[index], options)
		if code != "" {
			f.Errors = append(f.Errors, code)
		}
		output, _ := json.Marshal(processed)
		outputBudget += len(output)
		if outputBudget > 8192 {
			f.Truncated = true
			f.Errors = append(f.Errors, "VALUE_BUDGET_EXCEEDED")
			break
		}
		values = append(values, processed)
	}
	if truncated {
		f.Errors = append(f.Errors, "MATCH_BUDGET_REACHED")
	}
	if !options.Multiple && len(raw) > 1 {
		f.Errors = append(f.Errors, "MULTIPLE_MATCHES")
	} else if options.Multiple {
		f.Value = values
	} else if len(values) > 0 {
		f.Value = values[0]
	}
	empty := field.Empty(f.Value)
	if options.Multiple {
		empty = true
		for _, v := range values {
			if !field.Empty(v) {
				empty = false
				break
			}
		}
	}
	if options.Required && empty {
		f.Errors = append(f.Errors, "REQUIRED_EMPTY")
	}
	if len(raw) == 0 && !options.Required {
		f.Value = nil
		if options.Multiple {
			f.Value = []any{}
		}
	}
	// Stable error codes, no raw values in diagnostic messages.
	seen := map[string]bool{}
	codes := []string{}
	for _, code := range f.Errors {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	f.Errors = codes
	f.Valid = len(codes) == 0
	rawJSON, _ := json.Marshal(f.RawValues)
	valueJSON, _ := json.Marshal(f.Value)
	f.RawJSON = string(rawJSON)
	f.ValueJSON = string(valueJSON)
	return f
}

func CandidateValues(result Result) []map[string]any {
	values := []map[string]any{}
	for _, record := range result.Records {
		if record.Valid {
			values = append(values, record.Values)
		}
	}
	return values
}
