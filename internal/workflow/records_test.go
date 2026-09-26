package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/record"
)

func articleSchema() record.Schema {
	return record.Schema{Version: 1, UniqueKeyFields: []string{"url"}, EmptyValuePolicy: "preserve", Fields: []record.Field{{Key: "url", Type: "url", Required: true}, {Key: "title", Type: "string"}, {Key: "body", Type: "string"}, {Key: "published_at", Type: "datetime"}}}
}

func TestTemplatesProduceSchemaValidCandidates(t *testing.T) {
	templates := Templates()
	for _, template := range templates {
		encoded, _ := json.Marshal(template.Definition)
		definition, err := ParseExecutableDefinition(string(encoded))
		if err != nil {
			t.Fatal(template.Code, err)
		}
		document := FetchResult{HTML: `<link rel="canonical" href="https://example.org/article"><h1>Title</h1><article>Body</article>`}
		want := 1
		role := "trigger"
		if template.Code == "json_api" {
			document = FetchResult{JSON: `{"items":[{"url":"https://example.org/a","title":" A ","body":"Body"},{"url":"https://example.org/b","title":"B","body":"Text"}]}`}
			want = 2
		}
		if definition.Listing != nil {
			role = "detail"
		}
		candidates, err := RecordCandidates(definition, document, role, articleSchema())
		if err != nil || len(candidates) != want {
			t.Fatalf("%s: %#v %v", template.Code, candidates, err)
		}
		if template.Code == "json_api" && candidates[0]["title"] != "A" {
			t.Fatalf("conversion did not run: %#v", candidates)
		}
	}
}

func TestTraceRecordCandidatesMatchesExecution(t *testing.T) {
	d := Templates()[0].Definition
	doc := FetchResult{HTML: `<link rel="canonical" href="https://example.org/trace"><h1>  Trace  </h1><article>Body</article>`}
	values, steps, err := TraceRecordCandidates(d, doc, "trigger", articleSchema())
	if err != nil || len(values) != 1 || len(steps) != 3 {
		t.Fatalf("trace: %#v %#v %v", values, steps, err)
	}
	if steps[0].Values["title"] != "Trace" {
		t.Fatalf("unexpected extraction: %#v", steps[0])
	}
	if len(steps[0].Fields) == 0 || steps[0].Fields[1].MatchCount != 1 || steps[0].Fields[1].RulePath != "nodes[0].config.fields[1]" {
		t.Fatalf("missing field diagnostics: %#v", steps[0].Fields)
	}
	plain, err := RecordCandidates(d, doc, "trigger", articleSchema())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(values)
	b, _ := json.Marshal(plain)
	if string(a) != string(b) {
		t.Fatalf("preview and execution differ: %s / %s", a, b)
	}
}

func TestRecordBatchRejectsBadPathsAndMissingKeys(t *testing.T) {
	d := Templates()[1].Definition
	for _, body := range []string{`{"items":[]}`, `{}`, `{"items":[1]}`, `{"items":[{"url":"https://example.org/a","title":"ok"},{"title":"no key"}]}`} {
		if _, err := RecordCandidates(d, FetchResult{JSON: body}, "trigger", articleSchema()); err == nil {
			t.Fatalf("invalid batch accepted: %s", body)
		}
	}
	d.Nodes[0].Config["items_path"] = "$.items[*]"
	if err := d.ValidateExecutable(); err == nil {
		t.Fatal("unsupported wildcard accepted")
	}
	for _, path := range []string{"$.items[*]", "$..url", "$.items[?(@.x)]", "$.items[-1]"} {
		if _, err := jsonPathValue(nil, path); err == nil {
			t.Fatalf("unsupported path accepted: %s", path)
		}
	}
}

func TestRecordPublicationRequiresDatasetFields(t *testing.T) {
	d := Templates()[0].Definition
	schema := articleSchema()
	schema.UniqueKeyFields = []string{"missing"}
	if err := d.ValidateRecordSchema(schema, "trigger"); err == nil || !strings.Contains(err.Error(), "unique_key_fields.missing") {
		t.Fatalf("missing schema key: %v", err)
	}
	d.Nodes[0].Config["fields"].([]any)[0].(map[string]any)["name"] = "unknown"
	if err := d.ValidateRecordSchema(articleSchema(), "trigger"); err == nil {
		t.Fatal("missing required URL accepted")
	}
}

func TestJSONOptionalFieldsAndRequiredEmptyArrays(t *testing.T) {
	candidates, err := RecordCandidates(Templates()[1].Definition, FetchResult{JSON: `{"items":[{"url":"https://example.org/a"}]}`}, "trigger", articleSchema())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("optional fields: %#v %v", candidates, err)
	}
	if _, err := Extract(ExtractRequest{Content: `<article></article>`, Fields: []FieldRule{{Name: "links", Selector: "a", Multiple: true, Required: true}}}); err == nil {
		t.Fatal("required empty array accepted")
	}
	values, err := Extract(ExtractRequest{ContentType: "json", Content: `{"title":"Title:  Hello   World "}`, Fields: []FieldRule{{Name: "title", Selector: "$.title", Regex: `Title: (.*)`, Clean: "whitespace"}}})
	if err != nil || values["title"] != "Hello World" {
		t.Fatalf("JSON cleaning: %#v %v", values, err)
	}
}
