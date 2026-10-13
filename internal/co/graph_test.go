package co

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOO3GraphAndMapping(t *testing.T) {
	d := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "root", Stages: []TaskStage{{ID: "root", Name: "入口", CollectorVersionID: "v1", OnError: "continue", Save: SaveBinding{TableID: "t1"}}, {ID: "child", Name: "子阶段", CollectorVersionID: "v1", OnError: "continue", Save: SaveBinding{TableID: "t2"}}}, Edges: []TaskEdge{{ID: "edge", From: "root", To: "child", Port: "records", Emit: "valid"}}, Source: ManualSource{Kind: "manual", Inputs: []map[string]any{{"url": "https://example.com"}}}, OverlapPolicy: "skip", Budget: TaskBudget{MaxWorkItems: 100, MaxPending: 1, MaxOutputs: 100, BrowserSlots: 1, MaxDurationMS: 1000}}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	invalid := d
	invalid.Edges = []TaskEdge{{ID: "edge", From: "child", To: "root", Port: "records", Emit: "valid"}}
	if invalid.Validate() == nil {
		t.Fatal("cycle accepted")
	}
	invalid = d
	invalid.Stages = append([]TaskStage{}, d.Stages...)
	invalid.Stages[1].Save.TableID = "t1"
	if invalid.Validate() == nil {
		t.Fatal("same table accepted")
	}
	up := collector.Definition{InputFields: []collector.Field{{Key: "id", Type: "string", Required: true}}, Output: collector.Output{Fields: []collector.Field{{Key: "urls", Type: "url", Required: true, Multiple: true, EmptyAllowed: true}}}}
	down := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}, {Key: "id", Type: "string", Required: true}}, Fetch: collector.Fetch{Kind: "http_html", URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://example.com"}}}
	e := d.Edges[0]
	e.Expand = &EdgeExpand{Path: "output.urls"}
	e.Mapping = map[string]collector.Binding{"url": {Kind: "field", Path: "element"}, "id": {Kind: "field", Path: "input.id"}}
	if err := validateEdge(e, up, down); err != nil {
		t.Fatal(err)
	}
	rows, err := mapEdge(e, down, map[string]any{"id": "x"}, map[string]any{"urls": []any{"https://example.com/a", "https://example.com/a"}})
	if err != nil || len(rows) != 2 || rows[0].Error != nil || rows[0].ElementKey == rows[1].ElementKey {
		t.Fatal(rows, err)
	}
	empty, err := mapEdge(e, down, map[string]any{"id": "x"}, map[string]any{"urls": []any{}})
	if err != nil || len(empty) != 0 {
		t.Fatal("empty array failed", err)
	}
	e.Filter = &EdgeFilter{Path: "input.id", Op: "equal", Value: json.RawMessage(`"keep"`)}
	rows, err = mapEdge(e, down, map[string]any{"id": "skip"}, map[string]any{"urls": []any{"https://example.com/a"}})
	if err != nil || rows[0].Decision != "filtered" {
		t.Fatal(rows, err)
	}
	rows, err = mapEdge(e, down, nil, map[string]any{"urls": []any{"https://example.com/a"}})
	if err != nil || rows[0].Decision != "invalid" {
		t.Fatal("missing treated as filter", rows, err)
	}
	e.Filter = nil
	e.Mapping["id"] = collector.Binding{Kind: "template", Value: json.RawMessage(`"movie-{{input.id}}"`)}
	if err = validateEdge(e, up, down); err != nil {
		t.Fatal(err)
	}
	rows, err = mapEdge(e, down, map[string]any{"id": "x"}, map[string]any{"urls": []any{"https://example.com/a"}})
	if err != nil || rows[0].Values["id"] != "movie-x" {
		t.Fatal(rows, err)
	}
	e.Mapping["id"] = collector.Binding{Kind: "template", Value: json.RawMessage(`"{{process.env}}"`)}
	if validateEdge(e, up, down) == nil {
		t.Fatal("unrestricted template accepted")
	}
}
