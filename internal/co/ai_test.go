package co

import (
	"context"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/collector"
)

func aiFixture() (collector.Definition, aiSource, AICandidate) {
	d := collector.Definition{DefinitionVersion: "collector.v1", InterpreterVersion: collector.Interpreter, InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}}, Fetch: collector.Fetch{Kind: "http_json", URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://fixture.example.test"}}, Records: collector.Locator{Strategy: "jsonpath", Expression: "$[*]"}, Output: collector.Output{Port: "records", Fields: []collector.Field{{Key: "id", Type: "string", Required: true}}, IdentityFields: []string{"id"}, Mapping: []collector.Mapping{{Key: "id", Source: "element", Locator: &collector.Locator{Strategy: "jsonpath", Expression: "$.missing"}, Extract: "text"}}}, Pagination: collector.Pagination{Kind: "none", EndCondition: "single_page", Resume: "url_or_cursor", MaxPages: 1, MaxRecords: 100, MaxDurationMS: 1000}}
	s := aiSource{Definition: d, Port: "records", Input: map[string]any{"url": "https://fixture.example.test/data"}, Documents: []collector.Document{{URL: "https://fixture.example.test/data", Format: "json", Content: `[{"id":"a"},{"id":"b"}]`}}}
	c := AICandidate{Selectors: []AISelectorEdit{{Field: "id", Locator: collector.Locator{Strategy: "jsonpath", Expression: "$.id"}, Extract: "text", Reason: "existing ID"}}, Explanation: "仅建议已声明字段"}
	return d, s, c
}
func TestCOAISampleInterpreter(t *testing.T) {
	d, src, c := aiFixture()
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	r := AISuggestion{Kind: "selectors", Source: collector.JSON(src)}
	v := validateAICandidate(context.Background(), r, c)
	if !v.Passed || v.Hash == "" {
		t.Fatal(v)
	}
	for _, tc := range []struct {
		name      string
		candidate AICandidate
	}{
		{"new_field", AICandidate{Selectors: []AISelectorEdit{{Field: "invented", Locator: c.Selectors[0].Locator, Extract: "text"}}}},
		{"wrong_format", AICandidate{Selectors: []AISelectorEdit{{Field: "id", Locator: collector.Locator{Strategy: "css", Expression: "a"}, Extract: "text"}}}},
		{"no_hit", AICandidate{Selectors: []AISelectorEdit{{Field: "id", Locator: collector.Locator{Strategy: "jsonpath", Expression: "$.absent"}, Extract: "text"}}}},
		{"unexpected_mapping", AICandidate{Selectors: c.Selectors, Bindings: []AIBindingEdit{{Field: "url", Path: "output.id"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if validateAICandidate(context.Background(), r, tc.candidate).Passed {
				t.Fatal("unsafe candidate accepted")
			}
		})
	}
	// Validation must not mutate the frozen definition (including shared slices).
	if collector.Hash(src.Definition) != collector.Hash(d) {
		t.Fatal("frozen source changed")
	}
	if validateAICandidate(context.Background(), AISuggestion{Kind: "failure", Source: collector.JSON(aiSource{})}, c).Passed {
		t.Fatal("failure suggestions accepted rules")
	}
}
func TestCOAIMappingScope(t *testing.T) {
	d, _, _ := aiFixture()
	up := d
	up.Output.Fields = []collector.Field{{Key: "url", Type: "url", Required: true}}
	src := aiSource{Definition: up, Down: d, Edge: TaskEdge{ID: "edge", Port: "records", Emit: "valid", Mapping: map[string]collector.Binding{}}, Output: map[string]any{"url": "https://fixture.example.test/next"}}
	r := AISuggestion{Kind: "mapping", Source: collector.JSON(src)}
	for _, path := range []string{"output.url", "input.unknown", "element", "output.url.nested"} {
		v := validateAICandidate(context.Background(), r, AICandidate{Bindings: []AIBindingEdit{{Field: "url", Path: path}}})
		if v.Passed != (path == "output.url") {
			t.Fatal(path, v)
		}
	}
}
func TestCOAIRedaction(t *testing.T) {
	v := aiRedactDocument("json", `{"token":"hidden","nested":{"password":"hidden"},"url":"https://example.test/a?session=hidden"}`)
	if strings.Contains(v, "hidden") {
		t.Fatal(v)
	}
	v = aiRedactDocument("html", `<script>hidden</script><form>hidden</form><a data-token="hidden" href="https://example.test/a?token=hidden">标题</a>`)
	if strings.Contains(v, "hidden") || !strings.Contains(v, "标题") {
		t.Fatal(v)
	}
}

func TestCOAIHTMLLinksAndAmbiguity(t *testing.T) {
	d, src, _ := aiFixture()
	d.Fetch.Kind = "http_html"
	d.Records = collector.Locator{Strategy: "css", Expression: "li"}
	d.Output.Fields[0].Type = "url"
	d.Output.Mapping[0].Locator = &collector.Locator{Strategy: "css", Expression: ".missing"}
	src.Definition = d
	src.Documents = []collector.Document{{URL: "https://fixture.example.test/list", Format: "html", Content: `<li><a href="/detail/1"><span>电影名称</span></a></li>`}}
	r := AISuggestion{Kind: "selectors", Source: collector.JSON(src)}
	c := AICandidate{Selectors: []AISelectorEdit{{Field: "id", Locator: collector.Locator{Strategy: "css", Expression: "a"}, Extract: "link"}}}
	validation := validateAICandidate(context.Background(), r, c)
	if !validation.Passed {
		t.Fatal(validation)
	}
	for _, loc := range []collector.Locator{{Strategy: "xpath", Expression: ".//a"}, {Strategy: "xpath", Expression: "//a"}} {
		c.Selectors[0].Locator = loc
		v := validateAICandidate(context.Background(), r, c)
		if v.Passed != (loc.Expression == ".//a") {
			t.Fatal(loc, v)
		}
	}
	c.Selectors[0].Locator = collector.Locator{Strategy: "css", Expression: "a"}
	src.Documents[0].Content = `<li><a href="/one">一</a><a href="/two">二</a></li>`
	r.Source = collector.JSON(src)
	if validateAICandidate(context.Background(), r, c).Passed {
		t.Fatal("ambiguous single field accepted")
	}
}

func TestCOAIAdditionalPortIsolation(t *testing.T) {
	d, _, c := aiFixture()
	before := collector.Hash(d)
	d.AdditionalOutputs = []collector.OutputPort{{Records: d.Records, Output: collector.Output{Port: "extra", Fields: append([]collector.Field{}, d.Output.Fields...), IdentityFields: []string{"id"}, Mapping: append([]collector.Mapping{}, d.Output.Mapping...)}}}
	updated, e := applyAISelectors(d, "extra", c.Selectors)
	if e != nil {
		t.Fatal(e)
	}
	primary := updated
	primary.AdditionalOutputs = nil
	if collector.Hash(primary) != before || d.AdditionalOutputs[0].Output.Mapping[0].Locator.Expression != "$.missing" || updated.AdditionalOutputs[0].Output.Mapping[0].Locator.Expression != "$.id" {
		t.Fatal("port edit escaped scope or mutated source")
	}
}
