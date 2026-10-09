// Package output defines the pure v2.2 logical-table, mapping and write-decision
// contract. Both save previews and future run writers must reuse this contract.
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/field"
	"github.com/nekoimi/scrapio/internal/sample"
)

type Field struct {
	Key      string `json:"field_key"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Multiple bool   `json:"multiple"`
}
type Schema struct {
	Fields    []Field  `json:"fields"`
	UniqueKey []string `json:"unique_key"`
}
type Mapping struct {
	Source string `json:"source_field_key"`
	Target string `json:"target_field_key"`
}
type Config struct {
	TableID       string    `json:"table_id"`
	SchemaVersion int       `json:"schema_version"`
	SchemaHash    string    `json:"schema_hash"`
	StepID        string    `json:"step_id"`
	Stage         string    `json:"stage"`
	Mapping       []Mapping `json:"mapping"`
	UpdatePolicy  string    `json:"update_policy"`
	EmptyPolicy   string    `json:"empty_policy"`
	CheckID       string    `json:"check_id"`
}

func DecodeSchema(raw []byte) (Schema, error) {
	var s Schema
	if len(raw) > 64*1024 {
		return s, errors.New("schema exceeds budget")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF {
		return s, errors.New("invalid logical table schema")
	}
	return s, s.Validate()
}
func (s Schema) Validate() error {
	if len(s.Fields) < 1 || len(s.Fields) > 30 || len(s.UniqueKey) < 1 || len(s.UniqueKey) > 5 {
		return errors.New("schema requires 1..30 fields and 1..5 unique-key fields")
	}
	keys := map[string]Field{}
	names := map[string]bool{}
	for _, f := range s.Fields {
		if err := (field.Options{Key: f.Key, Type: f.Type}).Validate(); err != nil || f.Key == "" {
			return errors.New("invalid field key/type")
		}
		if !regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_-]{0,63}$`).MatchString(f.Name) || names[f.Name] {
			return errors.New("field names must be valid and unique")
		}
		if _, has := keys[f.Key]; has {
			return errors.New("duplicate field key")
		}
		if f.Type == "" {
			return errors.New("explicit table field type required")
		}
		keys[f.Key] = f
		names[f.Name] = true
	}
	seen := map[string]bool{}
	for _, key := range s.UniqueKey {
		f, has := keys[key]
		if !has || seen[key] || f.Multiple || f.Type == "json" {
			return errors.New("unique keys must reference distinct scalar fields")
		}
		seen[key] = true
	}
	return nil
}
func SchemaHash(s Schema) string { raw, _ := json.Marshal(s); return capture.Hash(raw) }
func DecodeConfig(raw []byte) (Config, error) {
	var c Config
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, errors.New("invalid output configuration")
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if id, err := strconv.ParseInt(c.TableID, 10, 64); err != nil || id < 1 || c.SchemaVersion < 1 || len(c.SchemaHash) != 64 || c.StepID == "" || len(c.StepID) > 128 || c.Stage != "list" && c.Stage != "detail" || c.CheckID == "" {
		return errors.New("confirmed table/schema/step/role/check required")
	}
	if _, err := uuid.Parse(c.CheckID); err != nil {
		return errors.New("invalid output check ID")
	}
	if decoded, err := json.Marshal(c); err != nil || len(decoded) > 64*1024 {
		return errors.New("output configuration exceeds budget")
	}
	return ValidatePolicies(c.Mapping, c.UpdatePolicy, c.EmptyPolicy)
}
func ValidatePolicies(mapping []Mapping, update, empty string) error {
	if len(mapping) < 1 || len(mapping) > 30 || update != "update" && update != "keep_existing" || empty != "preserve_existing" && empty != "overwrite" {
		return errors.New("mapping and explicit update/empty policies required")
	}
	sources := map[string]bool{}
	targets := map[string]bool{}
	for _, m := range mapping {
		if (field.Options{Key: m.Source}).Validate() != nil || (field.Options{Key: m.Target}).Validate() != nil {
			return errors.New("invalid mapping field key")
		}
		if m.Source == "" || m.Target == "" || sources[m.Source] || targets[m.Target] {
			return errors.New("mapping must be one-to-one")
		}
		sources[m.Source] = true
		targets[m.Target] = true
	}
	return nil
}

type SourceField struct {
	Key      string
	Type     string
	Multiple bool
}

func SourceFields(plan extraction.Plan, stage string) []SourceField {
	out := []SourceField{}
	if plan.Kind == "json_records" {
		for _, f := range plan.JSON.Fields {
			key := f.Key
			if key == "" {
				key = f.Name
			}
			out = append(out, SourceField{key, f.Type, f.Multiple})
		}
	} else {
		fields := plan.HTML.Fields
		if stage == "detail" {
			fields = plan.HTML.DetailFields
		}
		for _, f := range fields {
			key := f.Key
			if key == "" {
				key = f.Name
			}
			kind := f.Type
			if kind == "" {
				kind = "string"
			}
			out = append(out, SourceField{key, kind, f.Multiple})
		}
	}
	return out
}

type Issue struct {
	Code   string `json:"code"`
	Source string `json:"source_field_key,omitempty"`
	Target string `json:"target_field_key,omitempty"`
}
type Compatibility struct {
	Compatible bool    `json:"compatible"`
	Issues     []Issue `json:"issues"`
	Warnings   []Issue `json:"warnings"`
}

func CheckCompatibility(schema Schema, mapping []Mapping, sources []SourceField) Compatibility {
	out := Compatibility{Compatible: true, Issues: []Issue{}, Warnings: []Issue{}}
	src := map[string]SourceField{}
	dst := map[string]Field{}
	mappedSrc := map[string]bool{}
	mappedDst := map[string]bool{}
	for _, f := range sources {
		src[f.Key] = f
	}
	for _, f := range schema.Fields {
		dst[f.Key] = f
	}
	for _, m := range mapping {
		source, hasSource := src[m.Source]
		target, hasTarget := dst[m.Target]
		if !hasSource {
			out.Issues = append(out.Issues, Issue{"SOURCE_FIELD_MISSING", m.Source, m.Target})
		}
		if !hasTarget {
			out.Issues = append(out.Issues, Issue{"TARGET_FIELD_MISSING", m.Source, m.Target})
		}
		if !hasSource || !hasTarget {
			continue
		}
		mappedSrc[m.Source] = true
		mappedDst[m.Target] = true
		if source.Multiple != target.Multiple {
			out.Issues = append(out.Issues, Issue{"MULTIPLE_TYPE_MISMATCH", m.Source, m.Target})
		}
		if source.Type == "" {
			out.Warnings = append(out.Warnings, Issue{"SOURCE_TYPE_DYNAMIC", m.Source, m.Target})
		} else if target.Type != "json" && source.Type != target.Type && !(source.Type == "integer" && target.Type == "number") {
			out.Issues = append(out.Issues, Issue{"FIELD_TYPE_MISMATCH", m.Source, m.Target})
		}
	}
	for _, f := range sources {
		if !mappedSrc[f.Key] {
			out.Warnings = append(out.Warnings, Issue{"SOURCE_NOT_SAVED", f.Key, ""})
		}
	}
	for _, f := range schema.Fields {
		if !mappedDst[f.Key] && !f.Nullable {
			out.Issues = append(out.Issues, Issue{"REQUIRED_TARGET_UNMAPPED", "", f.Key})
		}
	}
	for _, key := range schema.UniqueKey {
		if !mappedDst[key] {
			out.Issues = append(out.Issues, Issue{"UNIQUE_KEY_UNMAPPED", "", key})
		}
	}
	out.Compatible = len(out.Issues) == 0
	return out
}

type Prepared struct {
	Values     map[string]any `json:"values"`
	ValuesJSON string         `json:"values_json"`
	Key        string         `json:"canonical_key"`
	KeyJSON    string         `json:"key_json"`
	Hash       string         `json:"values_hash"`
}

func Prepare(schema Schema, mapping []Mapping, record extraction.Record) (Prepared, []Issue) {
	p := Prepared{Values: map[string]any{}}
	issues := []Issue{}
	if !record.Valid {
		issues = append(issues, Issue{Code: "EXTRACTION_INVALID"})
	}
	fields := map[string]extraction.FieldResult{}
	for _, f := range record.Fields {
		fields[f.Key] = f
	}
	mapped := map[string]string{}
	for _, m := range mapping {
		mapped[m.Target] = m.Source
	}
	for _, target := range schema.Fields {
		source := mapped[target.Key]
		f, has := fields[source]
		var value any
		if has {
			value = f.Value
		} else if source != "" {
			issues = append(issues, Issue{"SOURCE_FIELD_MISSING", source, target.Key})
		}
		if field.Empty(value) {
			value = nil
			if !target.Nullable {
				issues = append(issues, Issue{"REQUIRED_TARGET_EMPTY", source, target.Key})
			}
		} else if !matches(value, target) {
			issues = append(issues, Issue{"VALUE_TYPE_MISMATCH", source, target.Key})
		}
		p.Values[target.Key] = value
	}
	keys := []any{}
	for _, key := range schema.UniqueKey {
		v := p.Values[key]
		if field.Empty(v) {
			issues = append(issues, Issue{Code: "UNIQUE_KEY_EMPTY", Target: key})
		}
		keys = append(keys, v)
	}
	raw, _ := json.Marshal(p.Values)
	if len(raw) > 256*1024 {
		issues = append(issues, Issue{Code: "RECORD_BUDGET_EXCEEDED"})
	}
	p.ValuesJSON = string(raw)
	p.Hash = capture.Hash(sample.CanonicalJSON(raw))
	keyRaw, _ := json.Marshal(keys)
	p.KeyJSON = string(keyRaw)
	p.Key = capture.Hash(sample.CanonicalJSON(keyRaw))
	return p, issues
}
func matches(value any, f Field) bool {
	if f.Multiple {
		values, ok := value.([]any)
		if !ok {
			return false
		}
		f.Multiple = false
		for _, v := range values {
			if v != nil && !matches(v, f) {
				return false
			}
		}
		return true
	}
	switch f.Type {
	case "json":
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "date", "datetime":
		v, ok := value.(string)
		if !ok {
			return false
		}
		layout := "2006-01-02"
		if f.Type == "datetime" {
			layout = time.RFC3339
		}
		_, err := time.Parse(layout, v)
		return err == nil
	}
	return false
}
func DecodeValues(raw string) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.UseNumber()
	var values map[string]any
	if d.Decode(&values) != nil || values == nil {
		return nil, errors.New("invalid saved record values")
	}
	return values, nil
}

type Existing struct {
	RecordID string
	Values   map[string]any
	Revision int
}
type Decision struct {
	Index    int    `json:"index"`
	Decision string `json:"decision"`
	Prepared
	RecordID       string   `json:"record_id,omitempty"`
	RecordRevision int      `json:"record_revision,omitempty"`
	ChangedFields  []string `json:"changed_fields"`
	Issues         []Issue  `json:"issues"`
	Reason         string   `json:"reason,omitempty"`
}
type Preview struct {
	Ready           bool           `json:"ready"`
	Compatibility   Compatibility  `json:"compatibility"`
	Counts          map[string]int `json:"counts"`
	Rows            []Decision     `json:"rows"`
	Warnings        []string       `json:"warnings"`
	NetworkAccessed bool           `json:"network_accessed"`
	DryRun          bool           `json:"dry_run"`
}

func PreviewBatch(schema Schema, mapping []Mapping, update, empty string, result extraction.Result, compat Compatibility, existing map[string]Existing) Preview {
	out := Preview{Ready: compat.Compatible, Compatibility: compat, Counts: map[string]int{"created": 0, "updated": 0, "unchanged": 0, "invalid": 0, "conflict": 0}, Rows: []Decision{}, Warnings: []string{"DATABASE_STATE_MAY_CHANGE"}, DryRun: true}
	groups := map[string][]int{}
	for _, record := range result.Records {
		p, issues := Prepare(schema, mapping, record)
		row := Decision{Index: record.Index, Prepared: p, ChangedFields: []string{}, Issues: issues}
		if !compat.Compatible || len(issues) > 0 {
			row.Decision = "invalid"
			out.Ready = false
		} else {
			groups[p.Key] = append(groups[p.Key], len(out.Rows))
		}
		out.Rows = append(out.Rows, row)
	}
	for _, indices := range groups {
		different := false
		for _, index := range indices[1:] {
			if out.Rows[index].Hash != out.Rows[indices[0]].Hash {
				different = true
			}
		}
		if different {
			out.Ready = false
			for _, index := range indices {
				out.Rows[index].Decision = "conflict"
				out.Rows[index].Reason = "DUPLICATE_KEY_DIFFERENT_VALUES"
			}
			continue
		}
		row := &out.Rows[indices[0]]
		previous, has := existing[row.Key]
		if !has {
			row.Decision = "created"
			for key := range row.Values {
				row.ChangedFields = append(row.ChangedFields, key)
			}
			sort.Strings(row.ChangedFields)
		} else {
			row.RecordID = previous.RecordID
			row.RecordRevision = previous.Revision
			merged, changed := Merge(previous.Values, row.Values, empty)
			row.ChangedFields = changed
			if len(changed) == 0 {
				row.Decision = "unchanged"
				row.Values = merged
			} else if update == "keep_existing" {
				row.Decision = "unchanged"
				row.Reason = "KEEP_EXISTING"
				row.ChangedFields = []string{}
				row.Values = previous.Values
			} else {
				row.Decision = "updated"
				row.Values = merged
			}
			raw, _ := json.Marshal(row.Values)
			row.ValuesJSON = string(raw)
			row.Hash = capture.Hash(sample.CanonicalJSON(raw))
		}
		for _, index := range indices[1:] {
			out.Rows[index].Decision = "unchanged"
			out.Rows[index].Reason = "DUPLICATE_KEY_IDENTICAL"
			out.Rows[index].Values = row.Values
			out.Rows[index].ValuesJSON = row.ValuesJSON
			out.Rows[index].Hash = row.Hash
		}
	}
	for _, row := range out.Rows {
		out.Counts[row.Decision]++
	}
	if len(result.Records) == 0 {
		out.Ready = false
		out.Warnings = append(out.Warnings, "NO_CANDIDATES")
	}
	if result.Truncated {
		out.Ready = false
		out.Warnings = append(out.Warnings, "PREVIEW_INCOMPLETE")
	}
	return out
}
func Merge(previous, incoming map[string]any, empty string) (map[string]any, []string) {
	out := map[string]any{}
	for k, v := range previous {
		out[k] = v
	}
	changed := []string{}
	for k, v := range incoming {
		if empty == "preserve_existing" && field.Empty(v) {
			continue
		}
		a, _ := json.Marshal(v)
		b, _ := json.Marshal(previous[k])
		if string(sample.CanonicalJSON(a)) != string(sample.CanonicalJSON(b)) {
			changed = append(changed, k)
			out[k] = v
		}
	}
	sort.Strings(changed)
	return out, changed
}
func ValidateDefinition(raw []byte) error {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return errors.New("invalid definition")
	}
	if len(root["output"]) == 0 {
		return nil
	}
	_, err := DecodeConfig(root["output"])
	return err
}
func Invalid(message string) error { return fmt.Errorf("output: %s", message) }
