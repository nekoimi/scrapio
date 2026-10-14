package co

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
	"xorm.io/xorm"
)

// Executed only in O2's bounded disposable schema; tests JSONB diagnostics
// and the ownership filter without modifying public tables or accessing sites.
func checkO5TransformPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, runID string) {
	t.Helper()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	edge := TaskEdge{ID: "transform-check", Mapping: map[string]collector.Binding{"rating": {Kind: "field", Path: "output.rating"}}, Transforms: map[string]*collector.Transform{
		"rating": {Version: collector.TransformVersion, Input: collector.Field{Key: "value", Type: "string", Required: true}, Expression: `Number(value.replace("分",""))`, Samples: []collector.TransformSample{{Value: json.RawMessage(`"9.2分"`), Expected: json.RawMessage(`9.2`)}}},
	}}
	down := collector.Definition{InputFields: []collector.Field{{Key: "rating", Type: "number", Required: true}}, Fetch: collector.Fetch{Kind: "http_html", URL: collector.Binding{Kind: "constant", Value: json.RawMessage(`"https://example.com"`)}, AllowedOrigins: []string{"https://example.com"}}}
	rows, e := mapEdgeContext(ctx, edge, down, nil, map[string]any{"rating": "9.2分"}, false)
	must(e)
	bad, e := mapEdgeContext(ctx, edge, down, nil, map[string]any{"rating": "unknown"}, false)
	must(e)
	if rows[0].Decision != "ready" || bad[0].Decision != "invalid" {
		t.Fatal(rows, bad)
	}
	bad[0].Ordinal = 1
	rows = append(rows, bad[0])
	s := engine.NewSession()
	out := new(SavedOutput)
	ok, e := s.Where("run_id=?", runID).Get(out)
	s.Close()
	must(e)
	if !ok {
		t.Fatal("fixture output missing")
	}
	plan := DispatchPlan{ID: uuid.NewString(), RunID: runID, Namespace: out.Namespace, OutputID: out.ID, EdgeID: edge.ID, FromStage: "movie", ToStage: "diagnostic-fixture", MappingHash: collector.Hash(edge), RowsJSON: collector.JSON(rows), Total: 2, NextOrdinal: 2, Status: "delivered"}
	_, e = engine.Insert(&plan)
	must(e)
	for i, row := range rows {
		_, e = engine.Exec(`INSERT INTO co_dispatches(id,run_id,plan_id,output_id,edge_id,expansion_ordinal,element_key,status,input_snapshot,error) VALUES(?,?,?,?,?,?,?,?,CAST(? AS jsonb),CAST(? AS jsonb))`, uuid.NewString(), runID, plan.ID, out.ID, edge.ID, i, row.ElementKey, row.Decision, collector.JSON(row.Values), collector.JSON(row.Error))
		must(e)
	}
	page, e := RunPage(ctx, 1, runID, "dispatches", "", 20)
	must(e)
	items := page.(map[string]any)["items"].([]json.RawMessage)
	if len(items) != 2 {
		t.Fatal("diagnostic receipts missing", len(items))
	}
	for _, raw := range items {
		var row struct {
			Ordinal    int                                     `json:"expansion_ordinal"`
			Transforms map[string]*collector.TransformEvidence `json:"transforms"`
		}
		must(json.Unmarshal(raw, &row))
		proof := row.Transforms["rating"]
		if proof == nil || proof.Hash != collector.Hash(edge.Transforms["rating"]) {
			t.Fatal("stored transform evidence missing", row)
		}
		if row.Ordinal == 0 && (proof.Before != "9.2分" || collector.JSON(proof.After) != "9.2") {
			t.Fatal(proof)
		}
		if row.Ordinal == 1 && proof.Error == nil {
			t.Fatal("failure evidence missing")
		}
	}
	if _, e = RunPage(ctx, 2, runID, "dispatches", "", 20); e != ErrNotFound {
		t.Fatal("diagnostic owner isolation", e)
	}
}
