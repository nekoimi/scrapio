package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(q *http.Request) (*http.Response, error) { return r(q) }
func TestCOHTTPAdaptersNormalizeHTMLAndJSON(t *testing.T) {
	for _, format := range []string{"html", "json"} {
		t.Run(format, func(t *testing.T) {
			input := map[string]any{"url": "https://fixture.example.test/", "movie_id": "m001"}
			body := "<main><h1>Title</h1></main>"
			d := collector.Definition{DefinitionVersion: "collector.v1", InterpreterVersion: collector.Interpreter, InputFields: []collector.Field{{Key: "url", Type: "url", Required: true}, {Key: "movie_id", Type: "string", Required: true}}, Fetch: collector.Fetch{Kind: "http_" + format, URL: collector.Binding{Kind: "field", Path: "input.url"}, AllowedOrigins: []string{"https://fixture.example.test"}}, Records: collector.Locator{Strategy: "css", Expression: "main"}, Output: collector.Output{Port: "records", Fields: []collector.Field{{Key: "movie_id", Type: "string", Required: true}, {Key: "title", Type: "string", Required: true}}, IdentityFields: []string{"movie_id"}, Mapping: []collector.Mapping{{Key: "movie_id", Source: "input", InputKey: "movie_id"}, {Key: "title", Source: "element", Locator: &collector.Locator{Strategy: "css", Expression: "h1"}, Extract: "text"}}}, Pagination: collector.Pagination{Kind: "none", EndCondition: "single_page", Resume: "url_or_cursor", MaxPages: 1, MaxRecords: 10, MaxDurationMS: 1000}}
			if format == "json" {
				body = `[{"title":"Title"}]`
				d.Records = collector.Locator{Strategy: "jsonpath", Expression: "$[*]"}
				d.Output.Mapping[1].Locator = &collector.Locator{Strategy: "jsonpath", Expression: "$.title"}
			}
			calls := 0
			s := &Source{Owner: 1, Definition: d, Mode: "live", HTTPClient: &http.Client{Transport: roundTrip(func(q *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: q}, nil
			})}}
			r := collector.Execute(context.Background(), d, input, "live", s)
			if r.Status != "succeeded" || calls != 1 || r.Candidates[0].Values["title"] != "Title" || r.FormalWrites != 0 {
				t.Fatal(r, calls)
			}
		})
	}
}

type browserStub struct {
	calls   int
	queries int
	closes  int
	err     error
	receipt drission_rod.EditorCommandResult
}

func (*browserStub) ProbeEditorIsolation(context.Context) error { return nil }
func (*browserStub) CreateEditorSession(context.Context, drission_rod.EditorSessionInput) (drission_rod.EditorSessionState, error) {
	return drission_rod.EditorSessionState{}, nil
}
func (*browserStub) GetEditorSession(context.Context, string) (drission_rod.EditorSessionState, error) {
	return drission_rod.EditorSessionState{SessionID: "s", Status: "ready", PageStateID: "p", CurrentURL: "https://fixture.example.test/"}, nil
}
func (b *browserStub) CloseEditorSession(context.Context, string) error { b.closes++; return nil }
func (*browserStub) InspectEditorPage(context.Context, string, string, string, editor.Inspection) (json.RawMessage, error) {
	return nil, nil
}
func (b *browserStub) ExecuteEditorCommand(context.Context, string, string, editor.Command) (drission_rod.EditorCommandResult, error) {
	b.calls++
	return drission_rod.EditorCommandResult{}, b.err
}
func (b *browserStub) GetEditorCommand(context.Context, string, string) (drission_rod.EditorCommandResult, error) {
	b.queries++
	if b.receipt.Status == "" {
		return b.receipt, &drission_rod.BrowserError{Code: "COMMAND_NOT_FOUND"}
	}
	return b.receipt, nil
}
func TestCOUnknownBrowserActionQueriesOnceNeverReplays(t *testing.T) {
	b := &browserStub{err: &drission_rod.BrowserError{Code: "UNAVAILABLE"}}
	s := &Source{Browser: b, ID: "s", Mode: "live", Definition: collector.Definition{Fetch: collector.Fetch{Kind: "browser", AllowedOrigins: []string{"https://fixture.example.test"}}}}
	e := s.Action(context.Background(), collector.Action{ID: "click", Kind: "click", Locator: &collector.Locator{Strategy: "css", Expression: "a"}, TimeoutMS: 1000}, "")
	if collector.AsError(e).Code != "OUTCOME_UNCERTAIN" || b.calls != 1 || b.queries != 1 {
		t.Fatal(e, b.calls, b.queries)
	}
	s.Close()
	if b.closes != 0 {
		t.Fatal("unknown receipt closed prematurely")
	}
	b.err = &drission_rod.BrowserError{Code: "INVALID_LOCATOR"}
	b.calls = 0
	b.queries = 0
	s.Uncertain = false
	e = s.Action(context.Background(), collector.Action{ID: "click", Kind: "click", Locator: &collector.Locator{Strategy: "css", Expression: "a"}, TimeoutMS: 1000}, "")
	if collector.AsError(e).Code != "INVALID_LOCATOR" || b.queries != 0 {
		t.Fatal(e, b.queries)
	}
}

func TestCOMissingBrowserBlocksWithoutPanic(t *testing.T) {
	d := collector.Definition{Fetch: collector.Fetch{Kind: "browser", AllowedOrigins: []string{"https://fixture.example.test"}}}
	s := &Source{Definition: d, Mode: "live"}
	_, e := s.Open(context.Background(), collector.Request{URL: "https://fixture.example.test/"})
	if e == nil || collector.AsError(e).Code != "BROWSER_UNAVAILABLE" {
		t.Fatal(e)
	}
}
