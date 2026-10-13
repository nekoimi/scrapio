package co

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
	"time"
)

func TestCOO2SchemaInputAndSavePolicies(t *testing.T) {
	schema := TableSchema{Fields: []collector.Field{{Key: "id", Type: "string", Required: true}, {Key: "title", Type: "string", Required: true}, {Key: "note", Type: "string", Nullable: true}}, UniqueKey: []string{"id"}}
	if e := schema.Validate(); e != nil {
		t.Fatal(e)
	}
	b := SaveBinding{TableID: "table", SchemaVersion: 1, SchemaHash: collector.Hash(schema), UniqueKey: schema.UniqueKey, UpdatePolicy: "merge_present", NullPolicy: "keep", Mapping: map[string]collector.Binding{"id": {Kind: "field", Path: "output.id"}, "title": {Kind: "field", Path: "output.title"}, "note": {Kind: "field", Path: "output.note"}}}
	values, key, e := mapValues(b, schema, map[string]any{"id": "x", "title": "first", "note": nil})
	if e != nil || key == "" {
		t.Fatal(values, key, e)
	}
	old := map[string]any{"id": "x", "title": "old", "note": "keep me"}
	effective := effectiveValues(old, values, b)
	if effective["note"] != "keep me" || effective["title"] != "first" {
		t.Fatal(effective)
	}
	b.NullPolicy = "clear"
	effective = effectiveValues(old, values, b)
	if effective["note"] != nil {
		t.Fatal(effective)
	}
	if _, _, e = mapValues(b, schema, map[string]any{"id": nil, "title": "x"}); e == nil {
		t.Fatal("null identity accepted")
	}
	// Absent differs from null, and output snapshots must not be replaced by merged rows.
	b.UpdatePolicy = "replace_present"
	effective = effectiveValues(old, map[string]any{"id": "x", "title": "new"}, b)
	if _, ok := effective["note"]; ok {
		t.Fatal(effective)
	}
	if _, ok := values["note"]; !ok {
		t.Fatal("mapping mutated output")
	}
	if canonicalHash(`{"long_key":1,"a":2}`) != canonicalHash(`{ "a":2, "long_key":1 }`) {
		t.Fatal("JSONB key ordering changed identity")
	}
}
func TestCOO2LeaseAndControlFence(t *testing.T) {
	until := time.Now().Add(time.Minute)
	r := Run{Status: "running", ControlEpoch: 4}
	item := WorkItem{Status: "running", LeaseToken: "new", LeaseUntil: &until, ControlEpoch: 4}
	c := WorkClaim{Item: WorkItem{LeaseToken: "old"}}
	if e := fence(&r, &item, &c); collector.AsError(e).Code != "LEASE_LOST" {
		t.Fatal(e)
	}
	c.Item.LeaseToken = "new"
	if e := fence(&r, &item, &c); e != nil {
		t.Fatal(e)
	}
	r.Status = "pausing"
	r.ControlEpoch++
	if e := fence(&r, &item, &c); e != nil {
		t.Fatal("authorized safe boundary rejected", e)
	}
	r.Status = "cancelled"
	if e := fence(&r, &item, &c); e == nil {
		t.Fatal("cancelled run accepted batch")
	}
	r.Status = "running"
	if e := fence(&r, &item, &c); e == nil {
		t.Fatal("old control epoch accepted")
	}
	d := collector.Definition{Fetch: collector.Fetch{Kind: "http_html", Method: "GET"}}
	problem := &collector.Error{Code: "HTTP_REQUEST_FAILED"}
	if !retryableRead(d, problem) {
		t.Fatal("transient GET not retryable")
	}
	d.Fetch.Method = "POST"
	if retryableRead(d, problem) {
		t.Fatal("POST replayed")
	}
	d.Fetch.Kind = "browser"
	d.Fetch.Method = "GET"
	if retryableRead(d, problem) {
		t.Fatal("browser auto replayed")
	}
	refs := `[{"command_id":"a","status":"pending"},{"command_id":"a","status":"resolved"},{"command_id":"b","status":"pending"}]`
	pending := unresolvedCommands(refs)
	if len(pending) != 1 || pending[0] != "b" {
		t.Fatal(pending)
	}
}
func TestCOO2TaskRejectsHistoricalOrGraphDefinitions(t *testing.T) {
	for _, raw := range []string{`{"definition_version":"task.v1","record_plans":[]}`, `{"definition_version":"task.v1","stages":[],"source":{"kind":"file"}}`} {
		var d TaskDefinition
		e := collector.Strict([]byte(raw), &d)
		if e == nil {
			e = d.Validate()
		}
		if e == nil {
			t.Fatal("legacy/incomplete definition accepted")
		}
	}
	d := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}}}
	rows, e := PreviewInputs(d, []map[string]any{{"url": "javascript:bad"}, {"url": "https://example.com", "cookie": "secret"}})
	if e != nil || len(rows) != 2 || rows[0].Error == nil || rows[1].Error == nil {
		t.Fatal(rows, e)
	}
	var raw any
	_ = json.Unmarshal([]byte(`{"a":1}`), &raw)
	if collector.Hash(raw) == "" {
		t.Fatal("hash missing")
	}
}
