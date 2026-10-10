package governance

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nekoimi/scrapio/internal/output"
)

func TestD05SchemaKeyNamespaceCannotChange(t *testing.T) {
	old := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}, {Key: "title", Name: "title", Type: "string", Nullable: true}}, UniqueKey: []string{"id"}}
	for _, change := range []func(*output.Schema){
		func(s *output.Schema) { s.UniqueKey = []string{"title"} },
		func(s *output.Schema) { s.Fields[0].Type = "string" },
		func(s *output.Schema) { s.Fields = s.Fields[1:]; s.UniqueKey = []string{"title"} },
		func(s *output.Schema) { s.Fields[0].Multiple = true },
	} {
		raw, _ := json.Marshal(old)
		var next output.Schema
		_ = json.Unmarshal(raw, &next)
		change(&next)
		if len(CompareSchema(old, next).Blockers) == 0 {
			t.Fatal("re-keying could alias existing canonical keys")
		}
	}
}
func TestD05SchemaChangesDoNotMutateHistoricalSchema(t *testing.T) {
	old := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "string"}, {Key: "title", Name: "title", Type: "string", Nullable: true}}, UniqueKey: []string{"id"}}
	raw, _ := json.Marshal(old)
	next := output.Schema{Fields: []output.Field{{Key: "id", Name: "external_id", Type: "string"}, {Key: "score", Name: "score", Type: "number", Nullable: true}}, UniqueKey: []string{"id"}}
	r := CompareSchema(old, next)
	if len(r.Blockers) != 0 || len(r.Changes) != 3 {
		t.Fatal(r)
	}
	if r.Changes[0].Before.Key != "id" || r.Changes[1].Before.Key != "title" {
		t.Fatal("field identity lost")
	}
	again, _ := json.Marshal(old)
	if string(raw) != string(again) {
		t.Fatal("immutable schema was changed")
	}
	if len(CompareSchema(old, old).Blockers) == 0 {
		t.Fatal("no-op must not create a schema version")
	}
}
func TestD05RetentionPolicyBudgetBoundaries(t *testing.T) {
	if !DefaultPolicy().Valid() {
		t.Fatal("default invalid")
	}
	for _, p := range []Policy{{-1, 7, 512}, {0, 0, 512}, {0, 366, 512}, {0, 7, 15}, {0, 7, 4097}} {
		if p.Valid() {
			t.Fatal("invalid limit accepted", p)
		}
	}
	for _, p := range []Policy{{0, 1, 16}, {10, 365, 4096}} {
		if !p.Valid() {
			t.Fatal(p)
		}
	}
	// Audit API is deliberately a struct allowlist instead of map[string]any.
	fields := reflect.TypeOf(SafeDetails{})
	for i := 0; i < fields.NumField(); i++ {
		kind := fields.Field(i).Type.Kind()
		if kind != reflect.Int && kind != reflect.Int64 {
			t.Fatal("audit details accepts arbitrary data")
		}
	}
}

func TestD05HistoricalSparsityAndTypesAreNotConverted(t *testing.T) {
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "string"}, {Key: "new", Name: "new", Type: "integer", Nullable: true}}, UniqueKey: []string{"id"}}
	values := map[string]any{"id": "external-1", "old": "retained"}
	if issues := HistoricalIssues(schema, values); len(issues) != 0 {
		t.Fatal("nullable new field is not incompatible", issues)
	}
	values["new"] = "42"
	issues := HistoricalIssues(schema, values)
	if len(issues) != 1 || issues[0].Code != "VALUE_TYPE_MISMATCH" || values["new"] != "42" || values["old"] != "retained" {
		t.Fatal("old values must not be converted or removed", issues, values)
	}
	delete(values, "new")
	schema.Fields[1].Nullable = false
	if issues := HistoricalIssues(schema, values); len(issues) == 0 {
		t.Fatal("new required field needs a visible historical violation")
	}
}
