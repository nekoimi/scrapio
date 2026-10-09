package sample

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/field"
	"testing"
)

func TestSamplePositiveNegativeAndPrecision(t *testing.T) {
	plan := extraction.Plan{StepID: "items", Kind: "json_records", JSON: capture.JSONPlan{ArrayPointer: "", MaxRecords: 20, Fields: []capture.JSONField{{Name: "identifier", Pointer: "/id", Options: field.Options{Key: "id-key", Type: "integer", Required: true}}}}}
	result, err := extraction.Extract(context.Background(), extraction.Input{Content: `[{"id":9007199254740993},{}]`, Format: "json", Stage: "list"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := DecodeExpected([]byte(`{"record_count":2,"valid_count":1,"invalid_count":1,"fields":[{"record_index":0,"field_key":"id-key","value":9007199254740993,"errors":[]},{"record_index":1,"field_key":"id-key","value":null,"errors":["REQUIRED_EMPTY"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	comparison := Compare(result, expected)
	if comparison.Status != "passed" {
		t.Fatalf("explicit negative sample failed: %+v", comparison)
	}
	expected.Fields[0].Value = json.RawMessage(`9007199254740992`)
	if Compare(result, expected).Status != "failed" {
		t.Fatal("large integer mismatch lost")
	}
	expected.Fields = expected.Fields[:1]
	if Compare(result, expected).Status != "failed" {
		t.Fatal("unexpected required failure passed")
	}
	if Compare(result, Expected{}).Status != "unconfigured" {
		t.Fatal("unset expectations pretended to pass")
	}
	if !equalJSON([]byte(`{"n":1.0,"items":[false,null,0]}`), []byte(`{"items":[false,null,0],"n":1}`)) {
		t.Fatal("JSON semantic equality broken")
	}
	if equalJSON([]byte(`{"n":null}`), []byte(`{"m":null}`)) {
		t.Fatal("missing and explicit null treated the same")
	}
	if !equalJSON([]byte(`1e999999999999999999999999`), []byte(`10e999999999999999999999998`)) {
		t.Fatal("large exponents expanded or compared incorrectly")
	}
}
func TestSampleMissingFieldAndTruncation(t *testing.T) {
	count := 1
	expected := Expected{RecordCount: &count, Fields: []FieldExpected{{RecordIndex: 0, FieldKey: "deleted", Value: json.RawMessage(`null`)}}}
	result := extraction.Result{MatchCount: 1, Records: []extraction.Record{{Index: 0, Fields: []extraction.FieldResult{{Key: "current", Valid: true, ValueJSON: `false`}}, Valid: true}}, ValidCount: 1}
	c := Compare(result, expected)
	if c.Status != "failed" || c.Differences[0].Code != "FIELD_NOT_FOUND" {
		t.Fatalf("deleted key not located: %+v", c)
	}
	expected.Fields = nil
	result.Truncated = true
	if Compare(result, expected).Status != "failed" {
		t.Fatal("partial coverage passed")
	}
	result.MatchCountLowerBound = true
	c = Compare(result, expected)
	if c.Differences[0].Code != "COUNT_INCOMPLETE" {
		t.Fatal("count lower bound treated as complete")
	}
}
func TestSampleExpectedValidation(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"unknown":true}`, `{"record_count":-1}`, `{"fields":[{"record_index":20,"field_key":"x","value":1}]}`, `{"fields":[{"record_index":0,"field_key":"x"}]}`, `{"fields":[{"record_index":0,"field_key":"x","value":1},{"record_index":0,"field_key":"x","value":2}]}`, `{} {}`} {
		if _, err := DecodeExpected([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid expectations: %s", raw)
		}
	}
}
