package v22_version_repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/sample"
	"github.com/nekoimi/scrapio/internal/trial"
)

func fixture(t *testing.T) (*state, publication.Capabilities) {
	t.Helper()
	definition := `{"definition_version":1,"entry_url":"https://example.com/items","http_request":{"method":"GET","timeout_ms":1000},"steps":[{"step_id":"items","type":"json_records","config":{"array_pointer":"","max_records":20,"fields":[{"field_key":"id","name":"id","pointer":"/id","type":"integer","required":true}]}}],"output":{"table_id":"1","schema_version":1,"schema_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","step_id":"items","stage":"list","mapping":[{"source_field_key":"id","target_field_key":"id"}],"update_policy":"update","empty_policy":"preserve_existing","check_id":"7911a460-e239-49f2-bd9c-777d81682d91"}}`
	plan, err := trial.Compile([]byte(definition), "json")
	if err != nil {
		t.Fatal(err)
	}
	schema := output.Schema{Fields: []output.Field{{Key: "id", Name: "id", Type: "integer"}}, UniqueKey: []string{"id"}}
	schemaRaw, _ := json.Marshal(schema)
	input := trial.Input{ExpectedRevision: 1, Mode: "live", Confirmed: true, Origins: []string{"https://example.com"}, Budget: trial.Budget{Seconds: 60, Pages: 3, Records: 20, Details: 1}}
	summary := trial.Summary{Status: "succeeded", Reason: "COMPLETED", Candidates: 1, Output: &output.Preview{Ready: true, Compatibility: output.Compatibility{Compatible: true}, Counts: map[string]int{"created": 1}, Rows: []output.Decision{{Decision: "created"}}}}
	now := time.Now()
	hash := publication.DefinitionHash(definition)
	st := &state{collector: &table.V22Collector{Revision: 1, Definition: definition}, plan: plan, schema: schema, input: input, summary: summary, trial: &table.V22Trial{Status: "succeeded", CollectorRevision: 1, DefinitionHash: hash, Definition: definition, OutputSchema: string(schemaRaw), FinishedAt: &now}, captures: map[string]table.V22Capture{}, manifest: manifest{Revision: 1, DefinitionHash: hash, Samples: []sampleIdentity{}}}
	for _, row := range []struct{ id, kind, content, expected string }{{"normal", "normal", `[{"id":9007199254740993}]`, `{"record_count":1,"fields":[{"record_index":0,"field_key":"id","value":9007199254740993}]}`}, {"missing", "missing_field", `[{}]`, `{"record_count":1,"fields":[{"record_index":0,"field_key":"id","errors":["REQUIRED_VALUE_MISSING"]}]}`}} {
		// Obtain the interpreter's actual required-value code rather than hardcoding it.
		expected := row.expected
		if row.kind == "missing_field" {
			res, err := extraction.Extract(context.Background(), extraction.Input{Content: row.content, Format: "json", Stage: "list"}, plan.Steps[0].Plan)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"record_count": 1, "fields": []any{map[string]any{"record_index": 0, "field_key": "id", "errors": res.Records[0].Fields[0].Errors}}})
			expected = string(raw)
		}
		contentHash := capture.Hash([]byte(row.content))
		expectedHash := capture.Hash(sample.CanonicalJSON([]byte(expected)))
		st.samples = append(st.samples, table.V22Sample{Id: row.id, Name: row.id, Revision: 1, StepId: "items", Stage: "list", Kind: row.kind, Expected: expected, ExpectedHash: expectedHash})
		st.captures[row.id] = table.V22Capture{Status: "succeeded", Content: row.content, ContentHash: contentHash, Format: "json", FinalURL: plan.URL}
		st.manifest.Samples = append(st.manifest.Samples, sampleIdentity{ID: row.id, Revision: 1, StepID: "items", Stage: "list", Kind: row.kind, ContentHash: contentHash, ExpectedHash: expectedHash})
	}
	return st, publication.Capabilities{DefinitionHash: hash, Contract: publication.ContractVersion, Interpreter: extraction.InterpreterVersion, HTTP: true, CredentialReady: true, Actions: []string{}}
}
func TestPublishReevaluatesAllSamplesAndNegativeExpectations(t *testing.T) {
	st, caps := fixture(t)
	result, proofs, err := evaluate(context.Background(), st, caps, false)
	if err != nil || !result.Ready || len(proofs) != 2 {
		t.Fatalf("valid evidence failed %+v %v", result, err)
	}
	raw, _ := json.Marshal(proofs)
	var restored []sampleProof
	if decode(raw, &restored) != nil || restored[0].Result.Records[0].Fields[0].Value != json.Number("9007199254740993") {
		t.Fatal("published evidence precision lost")
	}
	before := publication.Hash(st.manifest)
	st.manifest.Samples[0].Revision++
	if publication.Hash(st.manifest) == before {
		t.Fatal("sample revision not bound")
	}
	st.samples[0].Expected = `{"record_count":0}`
	st.samples[0].ExpectedHash = capture.Hash(sample.CanonicalJSON([]byte(st.samples[0].Expected)))
	result, _, err = evaluate(context.Background(), st, caps, false)
	if err != nil || result.Ready {
		t.Fatal("modified normal expectation passed", result, err)
	}
}
func TestPublishMissingSampleAndUnconfiguredAssertionsBlock(t *testing.T) {
	st, caps := fixture(t)
	st.samples = st.samples[:1]
	result, _, err := evaluate(context.Background(), st, caps, false)
	if err != nil || result.Ready {
		t.Fatal("missing negative sample passed")
	}
	st, caps = fixture(t)
	st.samples[1].Expected = `{}`
	st.samples[1].ExpectedHash = capture.Hash(sample.CanonicalJSON([]byte(`{}`)))
	result, _, err = evaluate(context.Background(), st, caps, false)
	if err != nil || result.Ready {
		t.Fatal("unconfigured sample passed")
	}
	st, caps = fixture(t)
	caps.DefinitionHash = "old"
	if _, _, err = evaluate(context.Background(), st, caps, false); err != ErrConflict {
		t.Fatal("capability/draft race accepted")
	}
}
func TestPublishInputBoundaries(t *testing.T) {
	if (CheckInput{ExpectedRevision: 1, TrialID: "not-an-id"}).Validate() == nil {
		t.Fatal("invalid trial ID accepted")
	}
	i := PublishInput{ExpectedRevision: 1, CheckID: "7911a460-e239-49f2-bd9c-777d81682d91", Note: "  first  "}
	if i.Validate() != nil || i.Note != "first" {
		t.Fatal("invalid note normalization")
	}
	if validateKey("") == nil {
		t.Fatal("missing publication key accepted")
	}
}
