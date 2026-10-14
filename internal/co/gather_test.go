package co

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func gatherFixture(mode string) (TaskDefinition, collector.Definition) {
	d := unionFixture()
	d.Stages = d.Stages[:4]
	d.Edges = d.Edges[:4]
	s := &d.Stages[3]
	s.InputMode = mode
	s.Gather = &GatherConfig{KeyFields: []string{"movie_id"}, Missing: "reject", MaxRows: 100, MaxBytes: 262144}
	down := collector.Definition{InputFields: []collector.Field{{Key: "movie_id", Type: "string", Required: true}, {Key: "url", Type: "url", Required: true}}, Fetch: collector.Fetch{Kind: "http_html", Method: "GET", URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://example.com"}}}
	d.Edges[2].Mapping = map[string]collector.Binding{"movie_id": {Kind: "field", Path: "output.movie_id"}, "url": {Kind: "field", Path: "output.url"}}
	d.Edges[3].Mapping = map[string]collector.Binding{"movie_id": {Kind: "field", Path: "output.movie_id"}}
	if mode == "batch" {
		s.Gather.KeyFields = nil
		down.InputFields = []collector.Field{{Key: "urls", Type: "url", Required: true, Multiple: true, EmptyAllowed: true}, {Key: "ids", Type: "string", Required: true, Multiple: true, EmptyAllowed: true}, {Key: "url", Type: "url", Required: true, Default: json.RawMessage(`"https://example.com/aggregate"`)}}
		d.Edges[2].Mapping = map[string]collector.Binding{"urls": {Kind: "field", Path: "output.url"}}
		d.Edges[3].Mapping = map[string]collector.Binding{"ids": {Kind: "field", Path: "output.movie_id"}}
	}
	return d, down
}
func TestCOO5GatherContractAndResults(t *testing.T) {
	d, down := gatherFixture("join")
	s := d.Stages[3]
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	if e := validateGatherMappings(d, s, down); e != nil {
		t.Fatal(e)
	}
	rows := []GatherRow{{ID: "b", EdgeID: "right_sink", Values: `{"movie_id":"m1"}`}, {ID: "a", EdgeID: "left_sink", Values: `{"movie_id":"m1","url":"https://example.com/m1"}`}}
	groups := gatherResults(d, s, down, rows)
	if len(groups) != 1 || groups[0].Status != "pending" || len(groups[0].Members) != 2 || groups[0].Values["url"] != "https://example.com/m1" {
		t.Fatal(groups)
	}
	groups = gatherResults(d, s, down, rows[:1])
	if groups[0].Error.Code != "JOIN_MISSING_SOURCE" || groups[0].Status != "invalid" {
		t.Fatal(groups)
	}
	s.Gather.Missing = "skip"
	groups = gatherResults(d, s, down, rows[:1])
	if groups[0].Status != "skipped" {
		t.Fatal(groups)
	}
	// Different keys produce independent work; no cross product or arbitrary
	// pairing of the other source's unrelated rows.
	multi := append(append([]GatherRow{}, rows...), GatherRow{ID: "c", EdgeID: "left_sink", Values: `{"movie_id":"m2","url":"https://example.com/m2"}`}, GatherRow{ID: "d", EdgeID: "right_sink", Values: `{"movie_id":"m2"}`})
	groups = gatherResults(d, s, down, multi)
	if len(groups) != 2 || groups[0].Status != "pending" || groups[1].Status != "pending" {
		t.Fatal(groups)
	}
	groups = gatherResults(d, s, down, append(rows, rows[0]))
	if groups[0].Error.Code != "JOIN_DUPLICATE_KEY" {
		t.Fatal(groups)
	}
	groups = gatherResults(d, s, down, []GatherRow{{ID: "nil", EdgeID: "left_sink", Values: `{"movie_id":null}`}})
	if groups[0].Status != "invalid" {
		t.Fatal(groups)
	}
	duplicate := d
	duplicate.Edges = append([]TaskEdge{}, d.Edges...)
	duplicate.Edges[3].Mapping = map[string]collector.Binding{"movie_id": {Kind: "field", Path: "output.movie_id"}, "url": {Kind: "field", Path: "output.url"}}
	if validateGatherMappings(duplicate, s, down) == nil {
		t.Fatal("ambiguous provider accepted")
	}
	d, down = gatherFixture("batch")
	s = d.Stages[3]
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	if e := validateGatherMappings(d, s, down); e != nil {
		t.Fatal(e)
	}
	groups = gatherResults(d, s, down, []GatherRow{{ID: "2", EdgeID: "left_sink", Values: `{"urls":"https://example.com/b"}`}, {ID: "1", EdgeID: "left_sink", Values: `{"urls":"https://example.com/a"}`}})
	if len(groups) != 1 || groups[0].Status != "pending" || len(groups[0].Values["ids"].([]any)) != 0 || groups[0].Values["urls"].([]any)[0] != "https://example.com/a" {
		t.Fatal(groups)
	}
	groups = gatherResults(d, s, down, nil)
	if len(groups) != 1 || groups[0].Status != "pending" || len(groups[0].Values["urls"].([]any)) != 0 {
		t.Fatal(groups)
	}
	s.Gather.MaxRows = 1001
	if validateGatherConfig(s, 2) == nil {
		t.Fatal("unbounded buffer")
	}
}
func TestCOO5GatherReplayBoundary(t *testing.T) {
	d, _ := gatherFixture("join")
	m := TaskManifest{Definition: d, Stages: map[string]FrozenStage{}}
	for _, s := range d.Stages {
		m.Stages[s.ID] = FrozenStage{}
	}
	if _, _, e := pruneManifest(m, "left"); e == nil {
		t.Fatal("missing barrier branch accepted")
	}
	p, _, e := pruneManifest(m, "sink")
	if e != nil || p.Stage("sink").Gather != nil || p.Stage("sink").InputMode != "single" {
		t.Fatal(e, p)
	}
	if d.Stages[3].Gather == nil {
		t.Fatal("history changed")
	}
}
