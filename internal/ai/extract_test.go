package ai

import "testing"

func TestParseResponseAndReviewStatus(t *testing.T) {
	result, err := ParseResponse(Response{Model: "demo", Content: `{"title":"demo","confidence":0.95}`}, map[string]any{"required": []any{"title"}, "properties": map[string]any{"title": map[string]any{"type": "string"}}})
	if err != nil || result.ReviewStatus != "pending_review" || result.Values["title"] != "demo" {
		t.Fatalf("unexpected AI result: %#v, %v", result, err)
	}
}

func TestParseResponseRejectsInvalidSchema(t *testing.T) {
	if _, err := ParseResponse(Response{Content: `{"title":3}`}, map[string]any{"properties": map[string]any{"title": map[string]any{"type": "string"}}}); err == nil {
		t.Fatal("expected schema type error")
	}
}
