package extraction

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/field"
	"strings"
	"testing"
)

func TestJSONPreviewConversionAndDiagnostics(t *testing.T) {
	plan := Plan{StepID: "items", Kind: "json_records", JSON: capture.JSONPlan{ArrayPointer: "", MaxRecords: 2, Fields: []capture.JSONField{
		{Name: "id", Pointer: "/id", Options: field.Options{Type: "integer", Required: true}},
		{Name: "title", Pointer: "/title", Options: field.Options{Type: "string", Clean: "whitespace", Required: true}},
		{Name: "tags", Pointer: "/tags", Options: field.Options{Type: "integer", Multiple: true}},
	}}}
	input := Input{Format: "json", Stage: "list", Content: `[{"id":9007199254740993,"title":"  First\n title ","tags":["1","2"]},{"id":"oops","tags":["3","bad"]},{"id":3}]`}
	result, err := Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchCountLowerBound || result.MatchCount != 3 || len(result.Records) != 2 || !result.Truncated || result.ValidCount != 1 || result.InvalidCount != 1 {
		t.Fatalf("bad counts %+v", result)
	}
	if result.Records[0].Fields[0].ValueJSON != "9007199254740993" || result.Records[0].Values["title"] != "First title" {
		t.Fatal("precision/cleaning lost")
	}
	bad := result.Records[1]
	if !contains(bad.Fields[0].Errors, "INVALID_INTEGER") || !contains(bad.Fields[1].Errors, "REQUIRED_EMPTY") || !contains(bad.Fields[2].Errors, "INVALID_INTEGER") {
		t.Fatalf("bad diagnostics %+v", bad)
	}
	if len(CandidateValues(result)) != 1 {
		t.Fatal("invalid records eligible for formal candidate writes")
	}
	second, err := Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(result)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("same saved input/plan is not deterministic")
	}
}

func TestHTMLPreviewRelativeScopesAndURL(t *testing.T) {
	plan := Plan{StepID: "items", Kind: "record_set", HTML: editor.RecordPlan{Mode: "repeated", MaxRecords: 10, Locator: &editor.Locator{Strategy: "css", Expression: ".item"}, Fields: []editor.FieldRule{
		{Name: "title", Extract: "text", Locator: &editor.Locator{Strategy: "xpath", Expression: ".//h2"}, Options: field.Options{Clean: "trim", Required: true}},
		{Name: "link", Extract: "link", Locator: &editor.Locator{Strategy: "css", Expression: ":scope > a"}},
		{Name: "flag", Extract: "attribute", Attribute: "data-flag", Options: field.Options{Type: "boolean"}},
	}}}
	input := Input{Format: "html", Stage: "list", URL: "https://example.com/list", Content: `<base href="/assets/"><div class="item" data-flag="false"><h2> One </h2><a href="one#part">one</a></div><div class="item" data-flag="true"><h2> Two </h2><a href="two">two</a></div>`}
	result, err := Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidCount != 2 || result.Records[0].Values["title"] != "One" || result.Records[1].Values["title"] != "Two" || result.Records[0].Values["link"] != "https://example.com/assets/one" || result.Records[0].Values["flag"] != false {
		t.Fatalf("relative scope/base/false mishandled %+v", result)
	}
	if result.Records[0].Fields[1].RawJSON != `["one#part"]` {
		t.Fatal("raw URL was replaced with resolved output")
	}
	plan.HTML.Fields[0].Locator.Expression = "./../../body//h2"
	result, err = Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidCount != 0 || !contains(result.Records[0].Fields[0].Errors, "INVALID_LOCATOR") {
		t.Fatal("relative XPath escaped record scope")
	}
}

func TestHTMLAmbiguousMissingAndDetail(t *testing.T) {
	plan := Plan{StepID: "items", Kind: "record_set", HTML: editor.RecordPlan{Mode: "single", MaxRecords: 1, Fields: []editor.FieldRule{
		{Name: "title", Extract: "text", Locator: &editor.Locator{Strategy: "css", Expression: "h2"}, Options: field.Options{Required: true}},
		{Name: "image", Extract: "image", Locator: &editor.Locator{Strategy: "css", Expression: "img"}, Options: field.Options{Required: true}},
		{Name: "absent", Extract: "text", Locator: &editor.Locator{Strategy: "css", Expression: ".absent"}},
	}, Detail: &editor.DetailRule{Locator: &editor.Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "back", MaxDetails: 1}, DetailFields: []editor.FieldRule{{Name: "date", Extract: "text", Locator: &editor.Locator{Strategy: "css", Expression: "time"}, Options: field.Options{Type: "date", DateFormat: "02/01/2006", Required: true}}}}}
	input := Input{Format: "html", Stage: "list", URL: "https://example.com/", Content: `<h2>A</h2><h2>B</h2><img><time>09/10/2026</time>`}
	result, err := Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	f := result.Records[0].Fields
	if !contains(f[0].Errors, "MULTIPLE_MATCHES") || !contains(f[1].Errors, "REQUIRED_EMPTY") || !f[2].Valid || result.ValidCount != 0 {
		t.Fatalf("bad missing/ambiguous checks %+v", f)
	}
	input.Stage = "detail"
	result, err = Extract(context.Background(), input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidCount != 1 || result.Records[0].Values["date"] != "2026-10-09" {
		t.Fatal("detail role did not use detail fields")
	}
}

func TestPreviewRejectsInvalidRulesAndBudgets(t *testing.T) {
	for _, raw := range []string{
		`{"definition_version":1,"steps":[{"step_id":"x","type":"json_records","config":{"array_pointer":"","max_records":1,"fields":[{"name":"x","pointer":"/x","clean":"unknown"}]}}]}`,
		`{"definition_version":1,"steps":[{"step_id":"x","type":"record_set","config":{"mode":"single","max_records":1,"fields":[{"name":"x","extract":"text","regex":"["}]}}]}`,
		`{"definition_version":1,"steps":[{"step_id":"x","type":"action"}]}`,
	} {
		if _, err := PlanFromDefinition([]byte(raw), "x"); err == nil {
			t.Fatal("invalid/unknown processing configuration accepted")
		}
	}
	plan := Plan{StepID: "items", Kind: "json_records", JSON: capture.JSONPlan{ArrayPointer: "", MaxRecords: 1, Fields: []capture.JSONField{{Name: "text", Pointer: "/text"}}}}
	content, _ := json.Marshal([]any{map[string]any{"text": strings.Repeat("x", 9000)}})
	result, err := Extract(context.Background(), Input{Format: "json", Stage: "list", Content: string(content)}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidCount != 0 || !result.Records[0].Fields[0].Truncated || !contains(result.Records[0].Fields[0].Errors, "VALUE_BUDGET_EXCEEDED") {
		t.Fatal("large field presented as valid")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Extract(ctx, Input{Format: "json", Stage: "list", Content: "[]"}, plan); err == nil {
		t.Fatal("cancelled preview still evaluated")
	}
}
func TestHTMLPreviewCountBounds(t *testing.T) {
	plan := Plan{StepID: "items", Kind: "record_set", HTML: editor.RecordPlan{Mode: "repeated", MaxRecords: 1, Locator: &editor.Locator{Strategy: "css", Expression: ".item"}, Fields: []editor.FieldRule{{Name: "title", Extract: "text"}}}}
	for _, count := range []int{2, 1001} {
		result, err := Extract(context.Background(), Input{Format: "html", Stage: "list", Content: strings.Repeat(`<div class="item">Item</div>`, count)}, plan)
		if err != nil {
			t.Fatal(err)
		}
		expected := count
		if count > 1000 {
			expected = 1000
		}
		if result.MatchCount != expected || result.MatchCountLowerBound != (count > 1000) || !result.Truncated || len(result.Records) != 1 {
			t.Fatalf("incorrect count bounds: %+v", result)
		}
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
