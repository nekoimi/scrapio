package workflow_repo

import "testing"

func TestSampleExpectationValidation(t *testing.T) {
	for _, tc := range []struct {
		outcome, errorText, role string
		valid                    bool
	}{
		{"", "", "trigger", true},
		{SampleError, "required field", "detail", true},
		{SampleEmptyList, "", "list", true},
		{SampleError, "", "detail", false},
		{SampleEmptyList, "", "detail", false},
		{SampleSuccess, "required", "trigger", false},
	} {
		_, err := sampleExpectation(tc.outcome, tc.errorText, tc.role)
		if (err == nil) != tc.valid {
			t.Fatalf("outcome %q, role %q: %v", tc.outcome, tc.role, err)
		}
	}
}

func TestSampleExpectationMatchesActualOutcome(t *testing.T) {
	result := SamplePreview{Error: `required field "url" is empty`}
	applyExpectation(&result, SampleError, "required field", SampleError)
	if !result.Passed || result.Error != "" || result.ActualError == "" {
		t.Fatalf("expected error did not match: %+v", result)
	}
	result = SamplePreview{Error: `required field "url" is empty`}
	applyExpectation(&result, SampleError, "other error", SampleError)
	if result.Passed || result.Error == "" {
		t.Fatalf("wrong error matched: %+v", result)
	}
	result = SamplePreview{}
	applyExpectation(&result, SampleSuccess, "", SampleEmptyList)
	if result.Passed {
		t.Fatalf("empty list matched a success expectation: %+v", result)
	}
}
