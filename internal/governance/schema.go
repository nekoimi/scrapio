// Package governance defines bounded, offline changes to the new product domain.
package governance

import (
	"encoding/json"
	"reflect"

	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
)

type FieldChange struct {
	Key    string        `json:"field_key"`
	Kind   string        `json:"kind"`
	Before *output.Field `json:"before,omitempty"`
	After  *output.Field `json:"after,omitempty"`
}
type SchemaChanges struct {
	Changes  []FieldChange `json:"changes"`
	Blockers []string      `json:"blockers"`
}

// Keys define the existing canonical-key namespace. Re-keying requires a new
// logical table; changing it in place could alias otherwise unrelated records.
func CompareSchema(before, after output.Schema) SchemaChanges {
	result := SchemaChanges{Changes: []FieldChange{}, Blockers: []string{}}
	old := map[string]output.Field{}
	next := map[string]output.Field{}
	for _, f := range before.Fields {
		old[f.Key] = f
	}
	for _, f := range after.Fields {
		next[f.Key] = f
	}
	if !reflect.DeepEqual(before.UniqueKey, after.UniqueKey) {
		result.Blockers = append(result.Blockers, "UNIQUE_KEY_CHANGE_REQUIRES_NEW_TABLE")
	}
	for _, key := range before.UniqueKey {
		a, b := old[key], next[key]
		if a.Type != b.Type || a.Multiple != b.Multiple || b.Key == "" {
			result.Blockers = append(result.Blockers, "UNIQUE_KEY_TYPE_CHANGE_REQUIRES_NEW_TABLE")
		}
	}
	for _, f := range before.Fields {
		n, has := next[f.Key]
		if !has {
			result.Changes = append(result.Changes, FieldChange{Key: f.Key, Kind: "removed", Before: &f})
		} else if f != n {
			result.Changes = append(result.Changes, FieldChange{Key: f.Key, Kind: "changed", Before: &f, After: &n})
		}
	}
	for _, f := range after.Fields {
		if _, has := old[f.Key]; !has {
			result.Changes = append(result.Changes, FieldChange{Key: f.Key, Kind: "added", After: &f})
		}
	}
	if output.SchemaHash(before) == output.SchemaHash(after) {
		result.Blockers = append(result.Blockers, "SCHEMA_UNCHANGED")
	}
	return result
}

// Historical values are checked as stored, never converted. An absent nullable
// new field is legitimate historical sparsity, not a missing extraction source.
func HistoricalIssues(schema output.Schema, values map[string]any) []output.Issue {
	record := extraction.Record{Valid: true}
	mapping := []output.Mapping{}
	for _, f := range schema.Fields {
		mapping = append(mapping, output.Mapping{Source: f.Key, Target: f.Key})
		record.Fields = append(record.Fields, extraction.FieldResult{Key: f.Key, Value: values[f.Key]})
	}
	_, issues := output.Prepare(schema, mapping, record)
	return issues
}

type Policy struct {
	Revision      int `json:"revision"`
	CaptureDays   int `json:"capture_days"`
	AssetLimitMiB int `json:"asset_limit_mib"`
}

func DefaultPolicy() Policy { return Policy{CaptureDays: 7, AssetLimitMiB: 512} }
func (p Policy) Valid() bool {
	return p.Revision >= 0 && p.CaptureDays >= 1 && p.CaptureDays <= 365 && p.AssetLimitMiB >= 16 && p.AssetLimitMiB <= 4096
}

// SafeDetails is an allowlist: never accept arbitrary request bodies, URLs,
// headers, user supplied labels, record values or credential material in audit.
type SafeDetails struct {
	FromVersion int   `json:"from_version,omitempty"`
	ToVersion   int   `json:"to_version,omitempty"`
	Revision    int   `json:"revision,omitempty"`
	Captures    int   `json:"captures,omitempty"`
	Snapshots   int   `json:"snapshots,omitempty"`
	Exports     int   `json:"exports,omitempty"`
	Bytes       int64 `json:"bytes,omitempty"`
}

func (d SafeDetails) JSON() string { raw, _ := json.Marshal(d); return string(raw) }
