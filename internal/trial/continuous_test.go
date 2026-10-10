package trial

import (
	"context"
	"errors"
	"fmt"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"strings"
	"testing"
)

type scriptedSource struct {
	docs        []FixedDocument
	reads       int
	actions     []editor.Command
	actionError error
}

func (*scriptedSource) Start(context.Context) error { return nil }
func (s *scriptedSource) Snapshot(context.Context) (FixedDocument, error) {
	if s.reads >= len(s.docs) {
		return FixedDocument{}, errors.New("UNEXPECTED_READ")
	}
	d := s.docs[s.reads]
	s.reads++
	return d, nil
}
func (s *scriptedSource) Action(_ context.Context, c editor.Command) error {
	s.actions = append(s.actions, c)
	return s.actionError
}
func (*scriptedSource) Close() {}
func htmlDoc(content, url string) FixedDocument {
	return FixedDocument{Content: content, Hash: capture.Hash([]byte(content)), URL: url, Format: "html"}
}
func loopRunner(docs ...FixedDocument) (Runner, *scriptedSource) {
	src := &scriptedSource{docs: docs}
	p := extraction.Plan{Kind: "record_set", StepID: "items", HTML: editor.RecordPlan{Mode: "repeated", Locator: &editor.Locator{Strategy: "css", Expression: ".item"}, MaxRecords: 200, Fields: []editor.FieldRule{{Name: "id", Extract: "text", Options: structOptions("id")}}, NextPage: &editor.NextPageRule{Kind: "link", Locator: &editor.Locator{Strategy: "css", Expression: ".next"}, MaxPages: 10}}}
	r := Runner{Continuous: true, Plan: Plan{URL: docs[0].URL, EntryType: "web", Steps: []Step{{ID: "items", Kind: "record_set", Plan: p}}, Output: output.Config{StepID: "items", Stage: "list", Mapping: []output.Mapping{{Source: "id", Target: "id"}}, UpdatePolicy: "update", EmptyPolicy: "preserve_existing"}}, Input: Input{Mode: "live", Budget: Budget{Seconds: 60, Pages: 30, Records: 100, Details: 5}, Origins: []string{"https://example.com"}}, Schema: output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "string"}}, UniqueKey: []string{"id"}}, Source: src}
	r.Emit = func(_ Event, d *Document) error {
		if d != nil {
			d.ID = fmt.Sprintf("doc-%d", src.reads)
		}
		return nil
	}
	return r, src
}
func TestContinuousLinksBeyondTwoPagesAndCheckpoint(t *testing.T) {
	r, src := loopRunner(htmlDoc(`<div class="item">one</div><a class="next" href="/2">next</a>`, "https://example.com/1"), htmlDoc(`<div class="item">two</div><a class="next" href="/3">next</a>`, "https://example.com/2"), htmlDoc(`<div class="item">three</div>`, "https://example.com/3"))
	checkpoints := []Checkpoint{}
	r.Emit = func(e Event, d *Document) error {
		if d != nil {
			d.ID = fmt.Sprintf("doc-%d", src.reads)
		}
		if e.Checkpoint != nil {
			checkpoints = append(checkpoints, *e.Checkpoint)
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Status != "succeeded" || result.Reason != "NO_NEXT_PAGE" || result.Candidates != 3 || len(src.actions) != 2 || result.ListPages != 3 {
		t.Fatalf("%+v actions %v", result, src.actions)
	}
	if result.LastCheckpoint == nil || result.LastCheckpoint.DocumentID != "doc-3" || result.LastCheckpoint.State != "stopped" || len(checkpoints) != 10 {
		t.Fatal(result.LastCheckpoint, checkpoints)
	}
}
func TestContinuousLoadMoreDedupAndRepeatedPage(t *testing.T) {
	d := func(items string) FixedDocument {
		return htmlDoc(items+`<button class="next">more</button>`, "https://example.com/list")
	}
	r, src := loopRunner(d(`<div class="item">one</div>`), d(`<div class="item">one</div><div class="item">two</div>`), d(`<header>different clock</header><div class="item">two</div><div class="item">one</div>`))
	r.Plan.Steps[0].Plan.HTML.NextPage.Kind = "load_more"
	r.Plan.Steps[0].Plan.HTML.NextPage.WaitMS = 300
	docs := []Document{}
	r.Emit = func(e Event, d *Document) error {
		if d != nil {
			d.ID = fmt.Sprintf("doc-%d", src.reads)
			docs = append(docs, *d)
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Status != "limited" || result.Reason != "REPEATED_PAGE" || result.Candidates != 2 || result.DuplicateRecords != 3 || len(src.actions) != 4 || src.actions[0].Type != "click" || src.actions[1].Type != "wait" {
		t.Fatalf("%+v actions %v", result, src.actions)
	}
	if docs[1].OutputIndices == nil || len(*docs[1].OutputIndices) != 1 || (*docs[1].OutputIndices)[0] != 1 || len(*docs[2].OutputIndices) != 0 {
		t.Fatal("diagnostic provenance lost", docs)
	}
}
func TestContinuousStopsEmptyRepeatedURLAndBudgets(t *testing.T) {
	first := htmlDoc(`<div class="item">one</div><a class="next" href="/2">next</a>`, "https://example.com/1")
	for _, test := range []struct {
		name, second, reason string
		budget               int
	}{
		{"empty", `<p>empty</p>`, "EMPTY_PAGE", 30},
		{"url", `<div class="item">two</div><a class="next" href="/1#top">again</a>`, "REPEATED_URL", 30},
		{"budget", `<div class="item">two</div>`, "PAGE_BUDGET_REACHED", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, src := loopRunner(first, htmlDoc(test.second, "https://example.com/2"))
			r.Input.Budget.Pages = test.budget
			result := r.Run(context.Background())
			if result.Reason != test.reason || result.Output == nil || len(result.Output.Rows) == 0 || result.FailedStage != "" {
				t.Fatal(result)
			}
			if src.reads > test.budget {
				t.Fatal("over budget")
			}
		})
	}
}
func TestContinuousNoNewRecordsAndRecordBudget(t *testing.T) {
	d := func(items string) FixedDocument {
		return htmlDoc(items+`<button class="next">more</button>`, "https://example.com/list")
	}
	r, _ := loopRunner(d(`<div class="item">one</div><div class="item">two</div>`), d(`<div class="item">one</div><div class="item">two</div><div class="item">one</div>`))
	r.Plan.Steps[0].Plan.HTML.NextPage.Kind = "click"
	if result := r.Run(context.Background()); result.Reason != "NO_NEW_RECORDS" || result.Candidates != 2 {
		t.Fatal(result)
	}
	r, src := loopRunner(d(`<div class="item">one</div><div class="item">two</div>`))
	r.Plan.Steps[0].Plan.HTML.NextPage.Kind = "load_more"
	r.Input.Budget.Records = 1
	if result := r.Run(context.Background()); result.Reason != "RECORD_BUDGET_REACHED" || result.Candidates != 1 || len(src.actions) != 0 {
		t.Fatal(result)
	}
}
func TestContinuousCancelAndUnknownActionNeverReplay(t *testing.T) {
	first := htmlDoc(`<div class="item">one</div><button class="next">next</button>`, "https://example.com/list")
	r, src := loopRunner(first)
	r.Plan.Steps[0].Plan.HTML.NextPage.Kind = "click"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Emit = func(e Event, d *Document) error {
		if d != nil {
			d.ID = "doc-1"
		}
		if e.Checkpoint != nil && e.Checkpoint.State == "action_pending" {
			cancel()
		}
		return nil
	}
	if result := r.Run(ctx); result.Status != "cancelled" || len(src.actions) != 0 {
		t.Fatal(result, src.actions)
	}
	r, src = loopRunner(first)
	r.Plan.Steps[0].Plan.HTML.NextPage.Kind = "click"
	src.actionError = errors.New("ACTION_OUTCOME_UNCERTAIN")
	result := r.Run(context.Background())
	if result.Output != nil || len(src.actions) != 1 || result.Reason != "ACTION_OUTCOME_UNCERTAIN" || result.LastCheckpoint.State != "action_pending" {
		t.Fatal(result, src.actions)
	}
}
func TestContinuousDetailURLDedupAndReturnState(t *testing.T) {
	list := func(id, next string) FixedDocument {
		return htmlDoc(`<div class="item"><a href="/detail">`+id+`</a></div>`+next, "https://example.com/"+id)
	}
	one := list("one", `<a class="next" href="/two">next</a>`)
	two := list("two", "")
	r, src := loopRunner(one, htmlDoc(`<h1>detail</h1>`, "https://example.com/detail"), one, two)
	r.Plan.Steps[0].Plan.HTML.Detail = &editor.DetailRule{Locator: &editor.Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "back", MaxDetails: 5}
	r.Plan.Steps[0].Plan.HTML.DetailFields = []editor.FieldRule{{Name: "id", Extract: "text", Options: structOptions("id")}}
	result := r.Run(context.Background())
	if result.Status != "succeeded" || result.DuplicateDetails != 1 || result.Details != 1 || len(src.actions) != 3 {
		t.Fatal(result, src.actions)
	}
	r, src = loopRunner(one, htmlDoc(`<h1>detail</h1>`, "https://example.com/detail"), htmlDoc(`<div class="item">lost</div>`, one.URL))
	r.Plan.Steps[0].Plan.HTML.Detail = &editor.DetailRule{Locator: &editor.Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "navigate-list", MaxDetails: 5}
	r.Plan.Steps[0].Plan.HTML.DetailFields = []editor.FieldRule{{Name: "id", Extract: "text", Options: structOptions("id")}}
	if result := r.Run(context.Background()); result.Reason != "RETURN_LIST_STATE_MISMATCH" || result.Output != nil {
		t.Fatal(result)
	}
}
func TestContinuousScopeAndRuleLimit(t *testing.T) {
	r, src := loopRunner(htmlDoc(`<div class="item">one</div><a class="next" href="https://else.example/">next</a>`, "https://example.com/list"))
	if result := r.Run(context.Background()); result.Reason != "NEXT_ORIGIN_OUT_OF_SCOPE" || len(src.actions) != 0 {
		t.Fatal(result)
	}
	r, src = loopRunner(htmlDoc(`<div class="item">one</div><a class="next" href="/two">next</a>`, "https://example.com/list"))
	r.Plan.Steps[0].Plan.HTML.NextPage.MaxPages = 1
	if result := r.Run(context.Background()); result.Reason != "LIST_PAGE_LIMIT_REACHED" || len(src.actions) != 0 {
		t.Fatal(result)
	}
	if URLKey("https://example.com/x?a=2#top") != "https://example.com/x?a=2" || !strings.Contains(URLKey("https://example.com/x?a=2"), "a=2") {
		t.Fatal("URL identity lost query")
	}
}

func TestContinuousConflictingKeysRemainBlocked(t *testing.T) {
	r, _ := loopRunner(htmlDoc(`<div class="item" data-id="7">old</div><a class="next" href="/2">next</a>`, "https://example.com/1"), htmlDoc(`<div class="item" data-id="7">new</div>`, "https://example.com/2"))
	r.Plan.Steps[0].Plan.HTML.Fields = []editor.FieldRule{{Name: "id", Extract: "attribute", Attribute: "data-id", Options: structOptions("id")}, {Name: "title", Extract: "text", Options: structOptions("title")}}
	r.Schema.Fields = append(r.Schema.Fields, output.Field{Key: "title", Name: "title", Type: "string"})
	r.Plan.Output.Mapping = append(r.Plan.Output.Mapping, output.Mapping{Source: "title", Target: "title"})
	result := r.Run(context.Background())
	if result.Status != "partial" || result.Candidates != 2 || result.Output.Counts["conflict"] != 2 {
		t.Fatal(result)
	}
}

func TestContinuousReturnIgnoresChromeButRetainsAccumulatedList(t *testing.T) {
	before := htmlDoc(`<header>clock1</header><div class="item">one</div><div class="item">two</div><a class="next" href="/2">next</a>`, "https://example.com/list")
	after := htmlDoc(`<header>clock2</header><div class="item">one</div><div class="item">two</div><a class="next" href="/2">next</a>`, before.URL)
	r, _ := loopRunner(before)
	if same, err := SameList(context.Background(), before, after, r.Plan.Steps[0].Plan); err != nil || !same {
		t.Fatal(same, err)
	}
	after = htmlDoc(`<div class="item">one</div><a class="next" href="/2">next</a>`, before.URL)
	if same, _ := SameList(context.Background(), before, after, r.Plan.Steps[0].Plan); same {
		t.Fatal("lost accumulated records accepted")
	}
}
