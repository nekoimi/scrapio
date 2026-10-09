package v22_sample_repo

import (
	"encoding/json"
	"testing"
)

func TestSampleInputAndStableExpectedHash(t *testing.T) {
	raw := `{"fields":[{"record_index":0,"field_key":"id","value":9007199254740993}],"record_count":1}`
	input := CreateInput{Name: " normal ", CaptureID: "7911a460-e239-49f2-bd9c-777d81682d91", ExpectedRevision: 1, StepID: "items", Stage: "list", Kind: "normal", ExpectedJSON: &raw}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	if input.Name != "normal" || input.ExpectedJSON != nil {
		t.Fatal("input not normalized")
	}
	formatted := `{ "record_count": 1, "fields": [ { "value": 9007199254740993, "field_key": "id", "record_index": 0 } ] }`
	if ExpectedHash(string(input.Expected)) != ExpectedHash(formatted) {
		t.Fatal("PostgreSQL JSONB reformat invalidates hash")
	}
	if ExpectedHash(`{"fields":[{"value":1e2}]}`) != ExpectedHash(`{"fields":[{"value":100}]}`) {
		t.Fatal("JSONB exponent expansion invalidates hash")
	}
	other := input
	other.Expected = json.RawMessage(formatted)
	if err := other.Validate(); err != nil {
		t.Fatal(err)
	}
	if Fingerprint(1, input) != Fingerprint(1, other) {
		t.Fatal("semantic replay conflict")
	}
	if Fingerprint(1, input) == Fingerprint(2, input) {
		t.Fatal("collector omitted from binding")
	}
	input.IncludeScreenshot = true
	if input.Validate() == nil {
		t.Fatal("unreviewed screenshot accepted")
	}
}
