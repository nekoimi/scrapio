package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOO5PortBindingsAndPassThrough(t *testing.T) {
	pass := SaveBinding{Mode: "pass_through"}
	if e := validateSaveMode(pass); e != nil {
		t.Fatal(e)
	}
	if collector.JSON(pass) != `{"mode":"pass_through"}` {
		t.Fatal("pass leaked empty table fields", collector.JSON(pass))
	}
	pass.TableID = "legacy"
	if validateSaveMode(pass) == nil {
		t.Fatal("pass accepted table")
	}
	pass = SaveBinding{Mode: "pass_through"}
	d := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "root", Stages: []TaskStage{
		{ID: "root", Name: "root", CollectorVersionID: "v1", Save: pass, AdditionalSaves: []PortSave{{Port: "links", Save: pass}}, OnError: "continue"},
		{ID: "child", Name: "child", CollectorVersionID: "v1", Save: pass, OnError: "continue"},
	}, Edges: []TaskEdge{{ID: "edge", From: "root", To: "child", Port: "links", Emit: "valid"}}}
	if e := d.validateGraph(); e != nil {
		t.Fatal(e)
	}
	d.Edges[0].Emit = "changed"
	if d.validateGraph() == nil {
		t.Fatal("pass accepted business-change emit")
	}
	d.Edges[0].Emit = "valid"
	d.Edges[0].Port = "undeclared"
	if d.validateGraph() == nil {
		t.Fatal("undeclared port accepted")
	}
	up := collector.Definition{Output: collector.Output{Port: "records", Fields: []collector.Field{{Key: "id", Type: "string", Required: true}}}, AdditionalOutputs: []collector.OutputPort{{Output: collector.Output{Port: "links", Fields: []collector.Field{{Key: "url", Type: "url", Required: true}}, IdentityFields: []string{"url"}}}}}
	down := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}}}
	edge := TaskEdge{ID: "edge", Port: "links", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.url"}}}
	if e := validateEdge(edge, up, down); e != nil {
		t.Fatal(e)
	}
	edge.Port = "records"
	if validateEdge(edge, up, down) == nil {
		t.Fatal("mapping used another port schema")
	}
	port, _ := up.ForPort("links")
	if validateOutputCandidate(port, collector.Candidate{Valid: true, Identity: "x", Values: map[string]any{"url": ""}}) == nil {
		t.Fatal("empty pass identity value accepted")
	}
	c := collector.Candidate{Port: "links", Valid: true, Values: map[string]any{"url": "https://example.com"}}
	if Compare([]collector.Candidate{c}, []map[string]any{collector.SampleValue(c)})["status"] != "matched" {
		t.Fatal("named sample failed to match")
	}
	if Compare([]collector.Candidate{c}, []map[string]any{{"port": "records", "values": c.Values}})["status"] == "matched" {
		t.Fatal("sample ignored output port")
	}
}
