package run

import (
	"encoding/json"
	"testing"

	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/trial"
)

func TestFormalRunRequiresNewScopeConfirmation(t *testing.T) {
	i := Input{VersionID: "be0aa19c-8bb3-4da4-9a91-b8690c0b6dde", Origins: []string{"https://example.com"}, Budget: trial.Budget{Seconds: 60, Pages: 5, Records: 20, Details: 2}}
	if i.Validate() == nil {
		t.Fatal("unconfirmed formal access accepted")
	}
	i.Confirmed = true
	if err := i.Validate(); err != nil {
		t.Fatal(err)
	}
	p := trial.Plan{URL: "https://another.example/items"}
	if trial.ValidateScope(p, i.Trial(4)) == nil {
		t.Fatal("published entrance outside new run origins accepted")
	}
	i.VersionID = "draft"
	if i.Validate() == nil {
		t.Fatal("draft used as version")
	}
}
func TestFormalCandidateProvenanceAndIntegerPrecision(t *testing.T) {
	var result extraction.Result
	if Decode(`{"records":[{"index":0,"valid":true,"values":{"id":9007199254740993}},{"index":1,"valid":true,"values":{"id":9007199254740994}}]}`, &result) != nil {
		t.Fatal("decode failed")
	}
	docs := []trial.Document{{ID: "list", StepID: "items", Stage: "list", Result: result}, {ID: "detail-a", StepID: "items", Stage: "detail", URL: "https://example.com/a", Result: result}, {ID: "other", StepID: "else", Stage: "detail", Result: result}, {ID: "detail-b", StepID: "items", Stage: "detail", URL: "https://example.com/b", Result: result}}
	r, origins := Select(docs, output.Config{StepID: "items", Stage: "detail"})
	if len(r.Records) != 4 || len(origins) != 4 || origins[2].DocumentID != "detail-b" || origins[2].RecordIndex != 0 || r.Records[2].Index != 2 || r.Records[0].Values["id"] != json.Number("9007199254740993") {
		t.Fatal("role, provenance or integer precision changed", r, origins)
	}
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}}, UniqueKey: []string{"id"}}
	r.Records[0].Fields = []extraction.FieldResult{{Key: "id", Value: r.Records[0].Values["id"], Valid: true}}
	prepared, issues := output.Prepare(schema, []output.Mapping{{Source: "id", Target: "id"}}, r.Records[0])
	if len(issues) > 0 || prepared.KeyJSON != "[9007199254740993]" {
		t.Fatal("unique key rounded", prepared, issues)
	}
}
func TestFormalOutputKeepsObservationsDistinctFromMergedValues(t *testing.T) {
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}, {Key: "title", Name: "title", Type: "string", Nullable: true}}, UniqueKey: []string{"id"}}
	mapping := []output.Mapping{{Source: "id", Target: "id"}, {Source: "title", Target: "title"}}
	result := extraction.Result{Records: []extraction.Record{{Index: 0, Valid: true, Values: map[string]any{"id": json.Number("9007199254740993"), "title": nil}}}}
	result.Records[0].Fields = []extraction.FieldResult{{Key: "id", Value: json.Number("9007199254740993"), Valid: true}, {Key: "title", Value: nil, Valid: true}}
	p, issues := output.Prepare(schema, mapping, result.Records[0])
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	existing := map[string]output.Existing{p.Key: {RecordID: "one", Revision: 2, Values: map[string]any{"id": json.Number("9007199254740993"), "title": "saved"}}}
	preview := output.PreviewBatch(schema, mapping, "update", "preserve_existing", result, output.Compatibility{Compatible: true}, existing)
	s := trial.Summary{Status: "succeeded", Output: &preview}
	if !Writable(s, preview) || preview.Rows[0].Decision != "unchanged" || preview.Rows[0].Values["title"] != "saved" || p.Values["title"] != nil {
		t.Fatal("unchanged or incoming observation lost", preview, p)
	}
	result.Records = append(result.Records, extraction.Record{Index: 1, Valid: true, Values: map[string]any{"id": json.Number("9007199254740993"), "title": "other"}, Fields: []extraction.FieldResult{{Key: "id", Value: json.Number("9007199254740993"), Valid: true}, {Key: "title", Value: "other", Valid: true}}})
	conflict := output.PreviewBatch(schema, mapping, "update", "preserve_existing", result, output.Compatibility{Compatible: true}, existing)
	if Writable(s, conflict) {
		t.Fatal("cross-document key conflict written")
	}
	result.Truncated = true
	result.Records = result.Records[:1]
	limited := output.PreviewBatch(schema, mapping, "update", "preserve_existing", result, output.Compatibility{Compatible: true}, existing)
	s.Status = "limited"
	if !Writable(s, limited) {
		t.Fatal("finite valid coverage cannot commit with limited status")
	}
	s.FailedStage = "next-page"
	if Writable(s, limited) {
		t.Fatal("failed path written")
	}
	s.FailedStage = ""
	s.Output = nil
	if Writable(s, limited) {
		t.Fatal("missing output written")
	}
}
