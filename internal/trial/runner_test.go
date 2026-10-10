package trial

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/field"
	"github.com/nekoimi/scrapio/internal/output"
)

func structOptions(key string) field.Options { return field.Options{Key: key, Type: "string"} }

func jsonRunner(t *testing.T) (Runner, FixedDocument) {
	t.Helper()
	raw := []byte(`{"definition_version":1,"entry_url":"https://example.com/items","http_request":{"method":"GET","timeout_ms":1000},"steps":[{"step_id":"items","type":"json_records","config":{"array_pointer":"","max_records":20,"fields":[{"name":"id","field_key":"id","pointer":"/id","type":"integer","required":true},{"name":"title","field_key":"title","pointer":"/title","type":"string"}]}}],"output":{"table_id":"1","schema_version":1,"schema_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","step_id":"items","stage":"list","mapping":[{"source_field_key":"id","target_field_key":"id"},{"source_field_key":"title","target_field_key":"title"}],"update_policy":"update","empty_policy":"preserve_existing","check_id":"7911a460-e239-49f2-bd9c-777d81682d91"}}`)
	plan, err := Compile(raw, "json")
	if err != nil {
		t.Fatal(err)
	}
	content := `[{"id":9007199254740993,"title":"A"},{"id":9007199254740993,"title":"B"}]`
	fixed := FixedDocument{Content: content, Hash: capture.Hash([]byte(content)), URL: plan.URL, Format: "json"}
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}, {Key: "title", Name: "title", Type: "string", Nullable: true}}, UniqueKey: []string{"id"}}
	return Runner{Plan: plan, Input: Input{ExpectedRevision: 1, Mode: "offline", Budget: Budget{Seconds: 5, Pages: 2, Records: 20}}, Schema: schema, Fixed: map[string]FixedDocument{"items:list": fixed}}, fixed
}
func TestTrialOfflineUsesSharedInterpreterAndConflicts(t *testing.T) {
	r, _ := jsonRunner(t)
	docs := []Document{}
	r.Emit = func(e Event, doc *Document) error {
		if doc != nil {
			docs = append(docs, *doc)
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Status != "partial" || result.Output == nil || result.Output.Counts["conflict"] != 2 || result.NetworkAccessed || !result.DryRun {
		t.Fatalf("unexpected result %+v", result)
	}
	if len(docs) != 1 || docs[0].Result.Interpreter != extraction.InterpreterVersion || docs[0].Result.Records[0].Fields[0].Value != json.Number("9007199254740993") {
		t.Fatal("trial extraction/precision diverged")
	}
}
func TestTrialCancellationStopsDerivation(t *testing.T) {
	r, _ := jsonRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.Emit = func(e Event, doc *Document) error {
		if doc != nil {
			cancel()
		}
		return nil
	}
	reads := 0
	r.Existing = func(context.Context, extraction.Result) (map[string]output.Existing, error) { reads++; return nil, nil }
	result := r.Run(ctx)
	if result.Status != "cancelled" || reads != 0 {
		t.Fatalf("work derived after cancel: %+v reads %d", result, reads)
	}
}
func TestTrialBudgetAndInputIntegrity(t *testing.T) {
	r, _ := jsonRunner(t)
	r.Input.Budget.Records = 1
	result := r.Run(context.Background())
	if result.Status != "limited" || result.Candidates != 1 {
		t.Fatal(result)
	}
	r, _ = jsonRunner(t)
	fixed := r.Fixed["items:list"]
	fixed.Content += " "
	r.Fixed["items:list"] = fixed
	result = r.Run(context.Background())
	if result.Reason != "INPUT_HASH_MISMATCH" {
		t.Fatal(result)
	}
	r, _ = jsonRunner(t)
	delete(r.Fixed, "items:list")
	if r.Run(context.Background()).Reason != "OFFLINE_INPUT_MISSING" {
		t.Fatal("missing offline input accepted")
	}
}
func TestTrialScopeAndConfirmation(t *testing.T) {
	r, _ := jsonRunner(t)
	r.Input.Mode = "live"
	r.Input.Origins = []string{"https://example.com"}
	if r.Input.Validate() == nil {
		t.Fatal("unconfirmed live accepted")
	}
	r.Input.Confirmed = true
	if err := r.Input.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateScope(r.Plan, r.Input); err != nil {
		t.Fatal(err)
	}
	if Allowed("https://example.com.evil/items", r.Input.Origins) || Allowed("https://example.com:444/items", r.Input.Origins) {
		t.Fatal("origin boundary lost")
	}
	r.Input.Origins = []string{"https://example.com/path"}
	if r.Input.Validate() == nil {
		t.Fatal("path allowed as origin")
	}
}

func TestTrialOfflineActionsNeverPassAsLive(t *testing.T) {
	r, _ := jsonRunner(t)
	fixed := r.Fixed["items:list"]
	fixed.Content = `[{"id":1,"title":"A"}]`
	fixed.Hash = capture.Hash([]byte(fixed.Content))
	r.Fixed["items:list"] = fixed
	r.Plan.Steps = append([]Step{{ID: "click", Kind: "action", Action: editor.Command{Type: "click"}}}, r.Plan.Steps...)
	result := r.Run(context.Background())
	if result.Status != "limited" || result.NetworkAccessed || result.Output == nil || !result.Output.Ready {
		t.Fatalf("offline action incorrectly passed: %+v", result)
	}
}

func TestTrialRejectsUnknownStepsAndMissingOutput(t *testing.T) {
	r, _ := jsonRunner(t)
	raw, _ := json.Marshal(map[string]any{"definition_version": 1, "entry_url": r.Plan.URL, "output": r.Plan.Output, "steps": []any{map[string]any{"step_id": "x", "type": "plugin", "config": map[string]any{}}}})
	if _, err := Compile(raw, "web"); err == nil || !strings.Contains(err.Error(), "unsupported trial step") {
		t.Fatal("unknown step not explicitly rejected", err)
	}
	for _, raw := range []string{
		`{"definition_version":1,"entry_url":"https://example.com","steps":[{"step_id":"x","type":"plugin"}]}`,
		`{"definition_version":1,"entry_url":"https://example.com","steps":[]}`,
	} {
		if _, err := Compile([]byte(raw), "web"); err == nil {
			t.Fatal("unsupported/incomplete plan accepted")
		}
	}
}

type fakeSource struct {
	docs    []FixedDocument
	actions []string
	closed  bool
}

func (f *fakeSource) Start(context.Context) error { return nil }
func (f *fakeSource) Snapshot(context.Context) (FixedDocument, error) {
	return f.docs[len(f.actions)], nil
}
func (f *fakeSource) Action(_ context.Context, c editor.Command) error {
	f.actions = append(f.actions, c.Type)
	return nil
}
func (f *fakeSource) Close() { f.closed = true }
func TestTrialLiveDetailReturnAndNextPath(t *testing.T) {
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "string"}}, UniqueKey: []string{"id"}}
	plan := extraction.Plan{StepID: "items", Kind: "record_set", HTML: editor.RecordPlan{Mode: "repeated", Locator: &editor.Locator{Strategy: "css", Expression: ".item"}, MaxRecords: 20, Fields: []editor.FieldRule{{Name: "id", Extract: "text", Options: structOptions("id")}}, Detail: &editor.DetailRule{Locator: &editor.Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "back", MaxDetails: 1}, DetailFields: []editor.FieldRule{{Name: "id", Extract: "text", Options: structOptions("id")}}, NextPage: &editor.NextPageRule{Locator: &editor.Locator{Strategy: "css", Expression: ".next"}, Kind: "link", MaxPages: 2}}}
	doc := func(content, url string) FixedDocument {
		return FixedDocument{Content: content, Hash: capture.Hash([]byte(content)), URL: url, Format: "html"}
	}
	list := doc(`<div class="item"><a href="/detail">one</a></div><a class="next" href="/page2">Next</a>`, "https://example.com/list")
	src := &fakeSource{docs: []FixedDocument{list, doc(`<h1>Detail</h1>`, "https://example.com/detail"), list, doc(`<div class="item"><a href="/detail2">two</a></div>`, "https://example.com/page2")}}
	r := Runner{Plan: Plan{URL: list.URL, EntryType: "web", Steps: []Step{{ID: "items", Kind: "record_set", Plan: plan}}, Output: output.Config{StepID: "items", Stage: "list", Mapping: []output.Mapping{{Source: "id", Target: "id"}}, UpdatePolicy: "update", EmptyPolicy: "preserve_existing"}}, Input: Input{Mode: "live", Origins: []string{"https://example.com"}, Budget: Budget{Pages: 5, Records: 20, Details: 1}}, Schema: schema, Source: src}
	result := r.Run(context.Background())
	if result.Status != "limited" || result.Candidates != 2 || len(src.actions) != 3 || src.actions[0] != "navigate" || src.actions[1] != "back" || src.actions[2] != "navigate" || !src.closed {
		t.Fatalf("paths/return failed %+v %+v", result, src.actions)
	}
}
