package co

import (
	"fmt"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func unionFixture() TaskDefinition {
	d := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "root", Source: ManualSource{Kind: "manual", Inputs: []map[string]any{{"url": "https://example.com"}}}, OverlapPolicy: "skip", Budget: TaskBudget{MaxWorkItems: 100, MaxOutputs: 100, MaxPending: 2, BrowserSlots: 1, MaxDurationMS: 60000}}
	for _, id := range []string{"root", "left", "right", "sink", "tail"} {
		d.Stages = append(d.Stages, TaskStage{ID: id, Name: id, CollectorVersionID: "v", Save: SaveBinding{Mode: "pass_through"}, OnError: "continue"})
	}
	d.Stages[3].InputMode = "union"
	for _, pair := range [][2]string{{"root", "left"}, {"root", "right"}, {"left", "sink"}, {"right", "sink"}, {"sink", "tail"}} {
		d.Edges = append(d.Edges, TaskEdge{ID: pair[0] + "_" + pair[1], From: pair[0], To: pair[1], Port: "records", Emit: "valid"})
	}
	return d
}

func TestCOO5UnionLimitsAndPorts(t *testing.T) {
	d := unionFixture()
	d.Stages[0].InputMode = "union"
	if d.Validate() == nil {
		t.Fatal("union root accepted")
	}
	d = unionFixture()
	d.Edges = d.Edges[1:]
	if d.Validate() == nil {
		t.Fatal("unreachable source accepted")
	}
	d = unionFixture()
	d.Stages[1].AdditionalSaves = []PortSave{{Port: "extra", Save: SaveBinding{Mode: "pass_through"}}}
	d.Edges = append(d.Edges, TaskEdge{ID: "extra_sink", From: "left", To: "sink", Port: "extra", Emit: "valid"})
	if e := d.Validate(); e != nil {
		t.Fatal("distinct upstream ports rejected", e)
	}
	if order, e := d.TopologicalStages(); e != nil || len(order) != 5 {
		t.Fatal("multi-port parent was visited twice", order, e)
	}
	d = unionFixture()
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("branch_%d", i)
		d.Stages = append(d.Stages, TaskStage{ID: id, Name: id, CollectorVersionID: "v", Save: SaveBinding{Mode: "pass_through"}, OnError: "continue"})
		d.Edges = append(d.Edges, TaskEdge{ID: "to_" + id, From: "root", To: id, Port: "records", Emit: "valid"}, TaskEdge{ID: id + "_sink", From: id, To: "sink", Port: "records", Emit: "valid"})
		if i == 2 {
			if e := d.Validate(); e != nil {
				t.Fatal("five sources rejected", e)
			}
		}
	}
	if d.Validate() == nil {
		t.Fatal("six sources accepted")
	}
}

func TestCOO5UnionGraphAndClosure(t *testing.T) {
	d := unionFixture()
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	order, e := d.TopologicalStages()
	if e != nil || len(order) != 5 || order[3] != "sink" || order[4] != "tail" {
		t.Fatal("not topological", order, e)
	}
	d.Stages[3].InputMode = ""
	if d.Validate() == nil {
		t.Fatal("implicit multi-source accepted")
	}
	d.Stages[3].InputMode = "join"
	if d.Validate() == nil {
		t.Fatal("unsupported join accepted")
	}
	d.Stages[3].InputMode = "union"
	d.Edges = append(d.Edges, TaskEdge{ID: "cycle", From: "tail", To: "left", Port: "records", Emit: "valid"})
	d.Stages[1].InputMode = "union"
	if d.Validate() == nil {
		t.Fatal("non-root cycle accepted")
	}
	d = unionFixture()
	d.Edges = append(d.Edges, d.Edges[2])
	d.Edges[len(d.Edges)-1].ID = "duplicate"
	if d.Validate() == nil {
		t.Fatal("duplicate route accepted")
	}
	d = unionFixture()
	states := map[string]string{"left": "succeeded", "right": "dispatching"}
	if closed, _ := upstreamClosure(d, "sink", states); closed {
		t.Fatal("fast parent closed union")
	}
	states["right"] = "succeeded"
	if closed, reason := upstreamClosure(d, "sink", states); !closed || reason != "upstream_succeeded" {
		t.Fatal(closed, reason)
	}
	states["right"] = "limited"
	if _, reason := upstreamClosure(d, "sink", states); reason != "upstream_limited" {
		t.Fatal(reason)
	}
	states["left"] = "failed"
	if _, reason := upstreamClosure(d, "sink", states); reason != "upstream_partial" {
		t.Fatal("failure hidden by limit", reason)
	}
	states["right"] = "paused"
	if closed, _ := upstreamClosure(d, "sink", states); closed {
		t.Fatal("paused source closed union")
	}
}

func TestCOO5UnionReplayScope(t *testing.T) {
	d := unionFixture()
	if _, e := replayEdge(d, "sink", ""); e == nil {
		t.Fatal("ambiguous replay accepted")
	}
	edge, e := replayEdge(d, "sink", "left_sink")
	if e != nil || edge.From != "left" {
		t.Fatal(edge, e)
	}
	if _, e := replayEdge(d, "sink", "root_left"); e == nil {
		t.Fatal("wrong target edge accepted")
	}
	m := TaskManifest{Definition: d, Stages: map[string]FrozenStage{}}
	for _, st := range d.Stages {
		m.Stages[st.ID] = FrozenStage{}
	}
	p, _, e := pruneManifest(m, "left")
	if e != nil {
		t.Fatal(e)
	}
	if p.Stage("right").ID != "" || p.Stage("sink").InputMode != "union" || len(p.Definition.Edges) != 2 {
		t.Fatal("wrong replay descendants", p)
	}
	if _, e := p.Definition.TopologicalStages(); e != nil {
		t.Fatal(e)
	}
	p, _, e = pruneManifest(m, "sink")
	if e != nil || p.Stage("sink").InputMode != "single" {
		t.Fatal("new entry still union", e)
	}
	if d.Stages[3].InputMode != "union" {
		t.Fatal("historical definition mutated")
	}
	// Union accepts independently complete input only; one source cannot fill
	// missing required fields from another source.
	down := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}, {Key: "id", Type: "string", Required: true}}, Fetch: collector.Fetch{Kind: "http_html", URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://example.com"}}}
	rows, e := mapEdge(TaskEdge{Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.url"}}}, down, nil, map[string]any{"url": "https://example.com"})
	if e != nil || len(rows) != 1 || rows[0].Decision != "invalid" {
		t.Fatal("partial input paired implicitly", rows, e)
	}
}
