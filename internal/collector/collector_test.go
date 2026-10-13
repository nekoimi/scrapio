package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	Collectors []struct {
		ID         string     `json:"id"`
		Definition Definition `json:"definition"`
	} `json:"collectors"`
	Samples []struct {
		CollectorID string           `json:"collector_id"`
		Input       map[string]any   `json:"input"`
		HTML        string           `json:"html"`
		Expected    []map[string]any `json:"expected"`
	} `json:"samples"`
}

func baseline(t *testing.T) fixture {
	t.Helper()
	raw, e := os.ReadFile("../../docs/项目文档v2.2/编排O0/fixtures/baseline.json")
	if e != nil {
		t.Fatal(e)
	}
	var f fixture
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if e = d.Decode(&f); e != nil {
		t.Fatal(e)
	}
	return f
}

type fakeSource struct {
	docs         []Document
	opened       int
	actions      int
	closed       bool
	actionError  error
	request      Request
	nextOnAction bool
}

func (s *fakeSource) Open(_ context.Context, r Request) (Document, error) {
	s.request = r
	if s.opened >= len(s.docs) {
		return Document{}, Fault("OFFLINE_PAGE_MISSING", "fetch", "missing")
	}
	d := s.docs[s.opened]
	s.opened++
	return d, nil
}
func (s *fakeSource) Action(_ context.Context, _ Action, _ string) error {
	s.actions++
	if s.nextOnAction && s.actionError == nil && s.opened < len(s.docs) {
		s.opened++
	}
	return s.actionError
}

func TestCOLoadMoreDeduplicatesCumulativeRecords(t *testing.T) {
	b := baseline(t)
	d := b.Collectors[0].Definition
	input := b.Samples[0].Input
	d.Pagination.Kind = "load_more"
	d.Pagination.Resume = "restart_item"
	d.Pagination.MaxPages = 3
	d.Pagination.MaxRecords = 3
	raw, e := os.ReadFile("../../docs/项目文档v2.2/编排O0/fixtures/list.html")
	if e != nil {
		t.Fatal(e)
	}
	first := string(raw)
	last := strings.Replace(first, "</main>", `<article class="movie"><span class="rank">3</span><a class="title" data-movie-id="m003" href="/subject/m003">third</a></article></main>`, 1)
	last = strings.Replace(last, `class="next"`, `class="finished"`, 1)
	s := &fakeSource{docs: []Document{{URL: input["url"].(string), Format: "html", Content: first}, {URL: input["url"].(string), Format: "html", Content: last}}, nextOnAction: true}
	result := Execute(context.Background(), d, input, "live", s)
	if result.Status != "succeeded" || len(result.Candidates) != 3 || s.actions != 1 {
		t.Fatalf("%+v actions=%d", result, s.actions)
	}
}
func (s *fakeSource) Snapshot(context.Context) (Document, error) { return s.docs[s.opened-1], nil }
func (s *fakeSource) Close()                                     { s.closed = true }
func movie(t *testing.T) (Definition, map[string]any, Document) {
	t.Helper()
	b := baseline(t)
	d := b.Collectors[1].Definition
	i := b.Samples[1].Input
	raw, e := os.ReadFile(filepath.Join("../../docs/项目文档v2.2/编排O0/fixtures", b.Samples[1].HTML))
	if e != nil {
		t.Fatal(e)
	}
	return d, i, Document{URL: i["url"].(string), Format: "html", Content: string(raw)}
}
func TestCOThreeIndependentCollectors(t *testing.T) {
	b := baseline(t)
	for i, c := range b.Collectors {
		t.Run(c.ID, func(t *testing.T) {
			if e := c.Definition.Validate(); e != nil {
				t.Fatal(e)
			}
			sample := b.Samples[i]
			raw, e := os.ReadFile(filepath.Join("../../docs/项目文档v2.2/编排O0/fixtures", sample.HTML))
			if e != nil {
				t.Fatal(e)
			}
			source := &fakeSource{docs: []Document{{URL: sample.Input["url"].(string), Format: "html", Content: string(raw)}}}
			result := Execute(context.Background(), c.Definition, sample.Input, "live", source)
			if c.Definition.Pagination.Kind == "none" {
				if result.Status != "succeeded" {
					t.Fatalf("%+v", result)
				}
			} else if result.Status != "partial" && result.Status != "limited" {
				t.Fatalf("missing second-page evidence not full success: %+v", result)
			}
			actual := []map[string]any{}
			for _, v := range result.Candidates {
				if !v.Valid {
					t.Fatalf("invalid %+v", v)
				}
				actual = append(actual, v.Values)
			}
			if Hash(actual) != Hash(sample.Expected) {
				t.Fatalf("actual %s expected %s", JSON(actual), JSON(sample.Expected))
			}
			if result.FormalWrites != 0 || !source.closed {
				t.Fatal("test isolation/cleanup")
			}
		})
	}
}
func TestCOInputErrorsBeforeAccess(t *testing.T) {
	d, input, doc := movie(t)
	cases := []map[string]any{{"url": input["url"]}, {"url": "javascript:alert(1)", "movie_id": "m001"}, {"url": input["url"], "movie_id": nil}, {"url": input["url"], "movie_id": ""}, {"url": input["url"], "movie_id": "m001", "extra": "x"}}
	for _, i := range cases {
		s := &fakeSource{docs: []Document{doc}}
		r := Execute(context.Background(), d, i, "live", s)
		if r.Status != "failed" || s.opened != 0 {
			t.Fatalf("invalid input accessed source %+v", r)
		}
	}
}
func TestCODefaultNullMissingAndSafeInteger(t *testing.T) {
	f := Field{Key: "count", Type: "integer", Required: true, Default: json.RawMessage(`2`)}
	d := Definition{InputFields: []Field{f}}
	out, e := ValidateInput(d, map[string]any{})
	if e != nil || JSON(out) != "{\"count\":2}" {
		t.Fatal(out, e)
	}
	if _, e = ValidateInput(d, map[string]any{"count": nil}); e == nil {
		t.Fatal("null must not receive default")
	}
	f.Nullable = true
	d.InputFields[0] = f
	out, e = ValidateInput(d, map[string]any{"count": nil})
	if e != nil || out["count"] != nil {
		t.Fatal(out, e)
	}
	for _, v := range []any{"2", json.Number("9007199254740992"), json.Number("1.2")} {
		if _, e = ValidateValue(v, f, false); e == nil {
			t.Fatal("unsafe integer accepted", v)
		}
	}
	if _, e = ValidateInput(Definition{InputFields: []Field{{Key: "data", Type: "json"}}}, map[string]any{"data": map[string]any{"password": "secret"}}); e == nil {
		t.Fatal("nested secret persisted")
	}
}
func TestCOBindingsPreflight(t *testing.T) {
	d, input, _ := movie(t)
	d.Fetch.Kind = "http_html"
	d.Fetch.Method = "POST"
	d.InputFields = append(d.InputFields, Field{Key: "payload", Type: "json", Required: true})
	input["payload"] = map[string]any{"id": "m001"}
	d.Fetch.URL = Binding{Kind: "template", Value: json.RawMessage(`"https://fixture.example.test/subject/{{input.movie_id}}"`)}
	d.Fetch.Query = map[string]Binding{"id": {Kind: "field", Path: "input.movie_id"}}
	d.Fetch.Body = &Binding{Kind: "field", Path: "input.payload"}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	r, e := Prepare(d, input)
	if e != nil || r.URL != "https://fixture.example.test/subject/m001" || r.HTTP.Query["id"] != "m001" || string(r.HTTP.Body) != "{\"id\":\"m001\"}" {
		t.Fatal(r, e)
	}
	d.Fetch.URL.Value = json.RawMessage(`"https://fixture.example.test/{{input.payload}}"`)
	if d.Validate() == nil {
		t.Fatal("json template allowed")
	}
}
func TestCOActionValuesValidateBeforeAccess(t *testing.T) {
	d, input, doc := movie(t)
	d.Actions = []Action{{ID: "bad_scroll", Kind: "scroll", Value: &Binding{Kind: "constant", Value: json.RawMessage(`"not-an-int"`)}, TimeoutMS: 1000, ReplayClass: "read_only"}}
	s := &fakeSource{docs: []Document{doc}}
	r := Execute(context.Background(), d, input, "live", s)
	if r.Status != "failed" || s.opened != 0 {
		t.Fatal(r, s.opened)
	}
}
func TestCOSingleFieldDoesNotSilentlyPickFirst(t *testing.T) {
	d, input, doc := movie(t)
	doc.Content = strings.Replace(doc.Content, "</h1>", "</h1><h1>wrong</h1>", 1)
	s := &fakeSource{docs: []Document{doc}}
	r := Execute(context.Background(), d, input, "live", s)
	if r.Status != "partial" || r.Candidates[0].Fields["title"].MatchCount != 2 {
		t.Fatalf("%+v", r)
	}
}
func TestCOTextInsideLinkAndRelativeXPath(t *testing.T) {
	d, input, doc := movie(t)
	for i := range d.Output.Mapping {
		if d.Output.Mapping[i].Key == "comments_url" {
			d.Output.Mapping[i].Locator = &Locator{Strategy: "xpath", Expression: ".//a/span"}
		}
	}
	doc.Content = strings.Replace(doc.Content, "短评入口</a>", "<span>短评入口</span></a>", 1)
	s := &fakeSource{docs: []Document{doc}}
	r := Execute(context.Background(), d, input, "live", s)
	if r.Status != "succeeded" {
		t.Fatalf("%+v", r)
	}
	for _, m := range d.Output.Mapping {
		if m.Extract == "link" && r.Candidates[0].Values[m.Key] != "https://fixture.example.test/subject/m001/comments" {
			t.Fatal(r.Candidates)
		}
	}
}
func TestCOJSONRecordsCursorAndRepeat(t *testing.T) {
	d, input, _ := movie(t)
	d.Fetch.Kind = "http_json"
	d.Records = Locator{Strategy: "jsonpath", Expression: "$.items[*]"}
	d.Output = Output{Port: "records", Fields: []Field{{Key: "id", Type: "string", Required: true}, {Key: "movie_id", Type: "string", Required: true}}, IdentityFields: []string{"id"}, Mapping: []Mapping{{Key: "id", Source: "element", Locator: &Locator{Strategy: "jsonpath", Expression: "$.id"}, Extract: "text"}, {Key: "movie_id", Source: "input", InputKey: "movie_id"}}}
	d.Pagination = Pagination{Kind: "cursor", Next: &Locator{Strategy: "jsonpath", Expression: "$.next"}, CursorParameter: "cursor", EndCondition: "next_missing", Resume: "url_or_cursor", MaxPages: 3, MaxRecords: 10, MaxDurationMS: 1000}
	s := &fakeSource{docs: []Document{{URL: input["url"].(string), Format: "json", Content: `{"items":[{"id":"c1"}],"next":"c2"}`}, {URL: input["url"].(string), Format: "json", Content: `{"items":[{"id":"c2"}],"next":null}`}}}
	r := Execute(context.Background(), d, input, "live", s)
	if r.Status != "succeeded" || len(r.Candidates) != 2 || s.request.HTTP.Query["cursor"] != "c2" {
		t.Fatalf("%+v", r)
	}
	s = &fakeSource{docs: []Document{{URL: input["url"].(string), Format: "json", Content: `{"items":[{"id":"c1"}],"next":"c2"}`}, {URL: input["url"].(string), Format: "json", Content: `{"items":[{"id":"c1"}],"next":"different"}`}}}
	r = Execute(context.Background(), d, input, "live", s)
	if r.Reason != "REPEATED_PAGE" {
		t.Fatal(r)
	}
}
func TestCOZeroRequiresExplicitEvidence(t *testing.T) {
	d, input, doc := movie(t)
	doc.Content = "<main>login required</main>"
	d.Records.Expression = ".record"
	r := Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Reason != "ZERO_RESULT_UNVERIFIED" {
		t.Fatal(r)
	}
	d.Pagination.EmptyMarker = &Locator{Strategy: "css", Expression: ".empty"}
	doc.Content = "<p class='empty'>No records</p>"
	r = Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Status != "succeeded" || len(r.Candidates) != 0 {
		t.Fatal(r)
	}
}
func TestCOBudgetAndUnknownAction(t *testing.T) {
	d, input, doc := movie(t)
	d.Pagination = Pagination{Kind: "url", Next: &Locator{Strategy: "css", Expression: "a.next"}, EndCondition: "next_missing", Resume: "url_or_cursor", MaxPages: 1, MaxRecords: 10, MaxDurationMS: 1000}
	doc.Content += "<a class='next' href='/next'>Next</a>"
	r := Execute(context.Background(), d, input, "live", &fakeSource{docs: []Document{doc}})
	if r.Status != "limited" || r.Checkpoint.NextURL != "https://fixture.example.test/next" {
		t.Fatal(r)
	}
	d.Actions = []Action{{ID: "click", Kind: "click", Locator: &Locator{Strategy: "css", Expression: "a"}, TimeoutMS: 1000, ReplayClass: "receipt_required"}}
	s := &fakeSource{docs: []Document{doc}, actionError: Fault("OUTCOME_UNCERTAIN", "click", "unknown")}
	r = Execute(context.Background(), d, input, "live", s)
	if r.Status != "awaiting_confirmation" || s.actions != 1 {
		t.Fatal(r, s.actions)
	}
}
func TestCOOfflineActionsNotClaimedComplete(t *testing.T) {
	d, input, doc := movie(t)
	d.Actions = []Action{{ID: "wait", Kind: "wait", Value: &Binding{Kind: "constant", Value: json.RawMessage(`"0"`)}, TimeoutMS: 1000, ReplayClass: "read_only"}}
	s := &fakeSource{docs: []Document{doc}}
	r := Execute(context.Background(), d, input, "offline", s)
	if r.Status != "limited" || s.actions != 0 {
		t.Fatal(r)
	}
}
func TestCOOldDefinitionAndJSONPathScriptsRejected(t *testing.T) {
	if _, e := Decode([]byte(`{"definition_version":2,"detail_fields":[]}`)); e == nil {
		t.Fatal("legacy schema accepted")
	}
	for _, s := range []string{"$[?(@.secret)]", "$.eval()", "$..id"} {
		if _, e := jsonTokens(s); e == nil {
			t.Fatal("unsupported JSONPath accepted", s)
		}
	}
}
