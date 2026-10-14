package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOEdgeTransformsSchemaEvidenceAndOrigin(t *testing.T) {
	up := collector.Definition{Output: collector.Output{Port: "records", Fields: []collector.Field{{Key: "id", Type: "string", Required: true}}}}
	down := collector.Definition{InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}}, Fetch: collector.Fetch{Kind: "http_html", URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://example.com"}}}
	tr := &collector.Transform{Version: collector.TransformVersion, Input: collector.Field{Key: "value", Type: "string", Required: true}, Expression: `"https://example.com/" + value`, Samples: []collector.TransformSample{{Value: json.RawMessage(`"123"`), Expected: json.RawMessage(`"https://example.com/123"`)}}}
	edge := TaskEdge{ID: "link", Port: "records", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.id"}}, Transforms: map[string]*collector.Transform{"url": tr}}
	if e := validateEdge(edge, up, down); e != nil {
		t.Fatal(e)
	}
	rows, e := mapEdgeContext(context.Background(), edge, down, nil, map[string]any{"id": "123"}, false)
	if e != nil || rows[0].Decision != "ready" || rows[0].Transforms["url"].After != "https://example.com/123" {
		t.Fatal(rows, e)
	}
	tr.Expression = `"https://outside.example/" + value`
	tr.Samples[0].Expected = json.RawMessage(`"https://outside.example/123"`)
	if e = validateEdge(edge, up, down); e != nil {
		t.Fatal(e)
	}
	rows, e = mapEdgeContext(context.Background(), edge, down, nil, map[string]any{"id": "123"}, false)
	if e != nil || rows[0].Decision != "invalid" || rows[0].Error.Code != "ORIGIN_OUT_OF_SCOPE" {
		t.Fatal(rows, e)
	}
	// Gather validates transformed fragments, and retains complete-input checks at sealing.
	tr.Expression = `"https://example.com/" + value`
	tr.Samples[0].Expected = json.RawMessage(`"https://example.com/123"`)
	rows, e = mapEdgeContext(context.Background(), edge, down, nil, map[string]any{"id": "123"}, true)
	if e != nil || rows[0].Decision != "ready" {
		t.Fatal(rows, e)
	}
	edge.Transforms["missing"] = tr
	if validateEdge(edge, up, down) == nil {
		t.Fatal("orphan transform accepted")
	}
	delete(edge.Transforms, "missing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, e = mapEdgeContext(ctx, edge, down, nil, map[string]any{"id": "123"}, false)
	if e != nil || rows[0].Error.Code != "TRANSFORM_CANCELLED" {
		t.Fatal(rows, e)
	}
}
