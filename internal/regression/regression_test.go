package regression

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/field"
	"github.com/nekoimi/scrapio/internal/sample"
)

func TestSemanticDifference(t *testing.T) {
	d, err := CompareJSON([]byte(`{"big":9007199254740993,"x/y":null,"a":1.0}`), []byte(`{"big":9007199254740992,"a":1}`))
	if err != nil || len(d.Items) != 2 || d.Items[0].Path != "/big" || d.Items[1].Path != "/x~1y" || d.Items[1].AfterPresent {
		t.Fatalf("precision/missing/null diff: %#v %v", d, err)
	}
	items := make([]int, 201)
	for i := range items {
		items[i] = i
	}
	a, _ := json.Marshal(items)
	d, err = CompareJSON(a, []byte(`[]`))
	if err != nil || len(d.Items) != 200 || !d.Truncated {
		t.Fatal("unbounded difference", d, err)
	}
}

const definition = `{"definition_version":1,"entry_url":"https://example.com","steps":[{"step_id":"records","type":"json_records","config":{"array_pointer":"","max_records":20,"fields":[{"name":"title","field_key":"title","pointer":"/title","type":"string"}]}}]}`

func document(content, expected string) Document {
	return Document{ID: "sample", Revision: 3, StepID: "records", Stage: "list", Format: "json", Content: content, ContentHash: capture.Hash([]byte(content)), Expected: json.RawMessage(expected), ExpectedHash: capture.Hash(sample.CanonicalJSON([]byte(expected)))}
}
func execute(s Snapshot) (Report, error) {
	return Execute(context.Background(), s, func(int) error { return nil })
}
func TestSameInputRegression(t *testing.T) {
	d := document(`[{"title":"hello","name":"changed"}]`, `{"record_count":1,"fields":[{"record_index":0,"field_key":"title","value":"changed"}]}`)
	// Only selector changes. Both versions consume the exact same frozen document.
	updated := []byte(strings.Replace(definition, `"pointer":"/title"`, `"pointer":"/name"`, 1))
	r, err := execute(Snapshot{Base: []byte(definition), Target: updated, Samples: []Document{d}})
	if err != nil || r.Verdict != "passed" || r.Samples[0].Base.Status != "failed" || len(r.Samples[0].Records.Items) != 1 || r.Samples[0].Records.Items[0].Path != "/records/0/fields/title/value" || r.NetworkAccessed || !r.DryRun {
		t.Fatalf("same input report %#v %v", r, err)
	}
}
func TestIncompleteAndUnconfigured(t *testing.T) {
	for _, tt := range []struct {
		name string
		d    []Document
		want string
	}{
		{"empty", nil, "unconfigured"},
		{"no expectations", []Document{document(`[{"title":"hello"}]`, `{}`)}, "unconfigured"},
		{"missing", []Document{{ID: "missing", Error: "INPUT_MISSING"}}, "incomplete"},
		{"failed", []Document{document(`[{"title":"hello"}]`, `{"record_count":2}`)}, "failed"},
		{"passed", []Document{document(`[{"title":"hello"}]`, `{"record_count":1}`)}, "passed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := execute(Snapshot{Target: []byte(definition), Samples: tt.d})
			if err != nil || r.Verdict != tt.want {
				t.Fatalf("%#v %v", r, err)
			}
		})
	}
	d := document(`[{"title":"hello"}]`, `{"record_count":1}`)
	d.ExpectedHash = "bad"
	r, err := execute(Snapshot{Target: []byte(definition), Samples: []Document{d}})
	if err != nil || r.Verdict != "incomplete" {
		t.Fatal("corrupt expectation accepted")
	}
	d = document(`[{"title":"hello"}]`, `{"record_count":1}`)
	d.Content = "[]"
	r, err = execute(Snapshot{Target: []byte(definition), Samples: []Document{d}})
	if err != nil || r.Verdict != "incomplete" {
		t.Fatal("mutated input accepted")
	}
	_, err = Execute(context.Background(), Snapshot{Target: []byte(definition), Samples: []Document{document(`[]`, `{}`)}}, func(int) error { return errors.New("cancelled") })
	if err == nil {
		t.Fatal("progress fencing failure ignored")
	}
}

func TestHTMLListDetailAndNegativeSamples(t *testing.T) {
	p := editor.RecordPlan{Mode: "single", MaxRecords: 1, Fields: []editor.FieldRule{{Name: "title", Extract: "text", Locator: &editor.Locator{Strategy: "css", Expression: "h1"}, Options: field.Options{Key: "title", Required: true}}}, Detail: &editor.DetailRule{Locator: &editor.Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "back", MaxDetails: 1}, DetailFields: []editor.FieldRule{{Name: "title", Extract: "text", Locator: &editor.Locator{Strategy: "css", Expression: "h2"}, Options: field.Options{Key: "title", Required: true}}}}
	raw, _ := json.Marshal(map[string]any{"definition_version": 1, "steps": []any{map[string]any{"step_id": "records", "type": "record_set", "config": p}}})
	list := document(`<h1>List</h1>`, `{"fields":[{"record_index":0,"field_key":"title","value":"List"}]}`)
	list.ID = "list"
	list.Format = "html"
	detail := document(`<h2>Detail</h2>`, `{"fields":[{"record_index":0,"field_key":"title","value":"Detail"}]}`)
	detail.ID = "detail"
	detail.Stage = "detail"
	detail.Format = "html"
	negative := document(`<p>missing title</p>`, `{"invalid_count":1,"fields":[{"record_index":0,"field_key":"title","errors":["REQUIRED_EMPTY"]}]}`)
	negative.ID = "negative"
	negative.Kind = "missing_field"
	negative.Format = "html"
	r, err := execute(Snapshot{Target: raw, Samples: []Document{list, detail, negative}})
	if err != nil || r.Verdict != "passed" || len(r.Samples) != 3 {
		t.Fatalf("HTML roles/negative expectations: %#v %v", r, err)
	}
	jsonInput := document(`[{"title":"json"}]`, `{"record_count":1}`)
	jsonInput.Stage = "detail"
	r, err = execute(Snapshot{Target: []byte(definition), Samples: []Document{jsonInput}})
	if err != nil || r.Verdict != "failed" || r.Samples[0].Target.ErrorCode != "EXTRACTION_FAILED" {
		t.Fatal("JSON detail role silently substituted", r, err)
	}
}

func TestTruncationAndLongValueDifference(t *testing.T) {
	content := `[{"title":"a"},{"title":"b"}]`
	raw := strings.Replace(definition, `"max_records":20`, `"max_records":1`, 1)
	r, err := execute(Snapshot{Target: []byte(raw), Samples: []Document{document(content, `{"record_count":2}`)}})
	if err != nil || r.Verdict != "incomplete" {
		t.Fatal("truncated extraction reported pass", r, err)
	}
	before := strings.Repeat("x", 1000) + "A"
	after := strings.Repeat("x", 1000) + "B"
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	diff, err := CompareJSON(a, b)
	if err != nil || len(diff.Items) != 1 || diff.Items[0].BeforeHash == diff.Items[0].AfterHash {
		t.Fatal("long value equality based on clipped snippets", diff, err)
	}
}
