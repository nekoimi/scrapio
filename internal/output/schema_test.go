package output

import (
	"encoding/json"
	"testing"

	"github.com/nekoimi/scrapio/internal/extraction"
)

func fixture() (Schema, []Mapping, Compatibility) {
	s := Schema{Fields: []Field{{Key: "id", Name: "identifier", Type: "integer"}, {Key: "title", Name: "title", Type: "string", Nullable: true}, {Key: "flag", Name: "flag", Type: "boolean", Nullable: true}}, UniqueKey: []string{"id"}}
	m := []Mapping{{"source-id", "id"}, {"source-title", "title"}, {"source-flag", "flag"}}
	compat := CheckCompatibility(s, m, []SourceField{{"source-id", "integer", false}, {"source-title", "string", false}, {"source-flag", "boolean", false}})
	return s, m, compat
}
func record(i int, id, title, flag any) extraction.Record {
	return extraction.Record{Index: i, Valid: true, Fields: []extraction.FieldResult{{Key: "source-id", Value: id, Valid: true}, {Key: "source-title", Value: title, Valid: true}, {Key: "source-flag", Value: flag, Valid: true}}}
}
func TestOutputKeyAndSchemaCompatibility(t *testing.T) {
	s, m, c := fixture()
	if err := s.Validate(); err != nil || !c.Compatible {
		t.Fatal("valid schema incompatible", err, c)
	}
	p, issues := Prepare(s, m, record(0, json.Number("9007199254740993"), "Title", false))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	other, _ := Prepare(s, m, record(1, json.Number("9007199254740992"), "Title", false))
	if p.Key == other.Key || p.KeyJSON != "[9007199254740993]" {
		t.Fatal("key precision lost")
	}
	s.UniqueKey = []string{"flag"}
	falseKey, issues := Prepare(s, m, record(0, json.Number("0"), "", false))
	if len(issues) != 0 || falseKey.Key == "" {
		t.Fatal("false/zero invalidated")
	}
	s.UniqueKey = []string{"id"}
	_, issues = Prepare(s, m, record(0, nil, "Title", false))
	if len(issues) == 0 {
		t.Fatal("empty key accepted")
	}
	bad := CheckCompatibility(s, m, []SourceField{{"source-id", "string", false}, {"source-title", "string", false}, {"source-flag", "boolean", true}})
	if bad.Compatible {
		t.Fatal("mismatched shared table accepted")
	}
	bad = CheckCompatibility(s, m[1:], []SourceField{{"source-title", "string", false}, {"source-flag", "boolean", false}})
	if bad.Compatible {
		t.Fatal("missing key mapping accepted")
	}
	if _, err := DecodeSchema([]byte(`{"fields":[],"unique_key":[]}`)); err == nil {
		t.Fatal("empty schema accepted")
	}
}
func TestOutputPreviewDecisionsAndConflicts(t *testing.T) {
	s, m, c := fixture()
	first := record(0, json.Number("1"), "New", false)
	p, _ := Prepare(s, m, first)
	existing := map[string]Existing{p.Key: {RecordID: "record-one", Revision: 2, Values: map[string]any{"id": json.Number("1"), "title": "Old", "flag": false}}}
	result := extraction.Result{Records: []extraction.Record{first, record(1, json.Number("2"), "Other", true)}, MatchCount: 2}
	preview := PreviewBatch(s, m, "update", "preserve_existing", result, c, existing)
	if !preview.Ready || preview.Counts["updated"] != 1 || preview.Counts["created"] != 1 || preview.Rows[0].ChangedFields[0] != "title" {
		t.Fatalf("bad decisions %+v", preview)
	}
	result.Records = []extraction.Record{record(0, json.Number("1"), nil, false)}
	preview = PreviewBatch(s, m, "update", "preserve_existing", result, c, existing)
	if preview.Counts["unchanged"] != 1 {
		t.Fatal("empty overwrote existing")
	}
	if preview.Rows[0].Values["title"] != "Old" {
		t.Fatal("preview did not show preserved values")
	}
	preview = PreviewBatch(s, m, "update", "overwrite", result, c, existing)
	if preview.Counts["updated"] != 1 {
		t.Fatal("overwrite ignored")
	}
	result.Records = []extraction.Record{first}
	preview = PreviewBatch(s, m, "keep_existing", "overwrite", result, c, existing)
	if preview.Counts["unchanged"] != 1 || preview.Rows[0].Reason != "KEEP_EXISTING" {
		t.Fatal("keep existing ignored")
	}
	result.Records = []extraction.Record{first, record(1, json.Number("1"), "Different", false)}
	preview = PreviewBatch(s, m, "update", "overwrite", result, c, nil)
	if preview.Ready || preview.Counts["conflict"] != 2 {
		t.Fatal("same key different values silently overwrote")
	}
	result.Records = []extraction.Record{first, record(1, json.Number("1"), "New", false)}
	preview = PreviewBatch(s, m, "update", "overwrite", result, c, nil)
	if !preview.Ready || preview.Counts["created"] != 1 || preview.Counts["unchanged"] != 1 {
		t.Fatal("identical duplicates counted twice")
	}
	result.Truncated = true
	if PreviewBatch(s, m, "update", "overwrite", result, c, nil).Ready {
		t.Fatal("partial coverage confirmed")
	}
	result.Truncated = false
	result.Records = nil
	if PreviewBatch(s, m, "update", "overwrite", result, c, nil).Ready {
		t.Fatal("zero candidates confirmed")
	}
}
func TestOutputConfigAndSemanticMerge(t *testing.T) {
	if _, err := DecodeConfig([]byte(`{"table_id":"-1"}`)); err == nil {
		t.Fatal("invalid binding accepted")
	}
	before := map[string]any{"n": json.Number("1e2"), "flag": true}
	merged, changes := Merge(before, map[string]any{"n": json.Number("100"), "flag": false}, "preserve_existing")
	if len(changes) != 1 || changes[0] != "flag" || merged["flag"] != false {
		t.Fatal("number notation or false mishandled")
	}
	if err := ValidatePolicies([]Mapping{{"a", "x"}, {"b", "x"}}, "update", "overwrite"); err == nil {
		t.Fatal("ambiguous mapping accepted")
	}
}

func TestOutputCompositeKey(t *testing.T) {
	s, m, _ := fixture()
	s.UniqueKey = []string{"id", "flag"}
	first, issues := Prepare(s, m, record(0, json.Number("9007199254740993"), "Title", false))
	if len(issues) != 0 || first.KeyJSON != "[9007199254740993,false]" {
		t.Fatal("invalid composite key", first.KeyJSON, issues)
	}
	other, _ := Prepare(s, m, record(1, json.Number("9007199254740993"), "Title", true))
	if first.Key == other.Key {
		t.Fatal("composite key ignored second component")
	}
	s.UniqueKey = []string{"flag", "id"}
	reordered, _ := Prepare(s, m, record(0, json.Number("9007199254740993"), "Title", false))
	if first.Key == reordered.Key {
		t.Fatal("composite key ignored schema key order")
	}
	s.UniqueKey = []string{"id", "id"}
	if s.Validate() == nil {
		t.Fatal("duplicate composite key field accepted")
	}
}
