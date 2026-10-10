package v22_version_repo

import (
	"github.com/nekoimi/scrapio/internal/regression"
	"testing"
)

func TestRegressionAcknowledgementBatch(t *testing.T) {
	current := []sampleIdentity{{ID: "a", Revision: 2, StepID: "step", Stage: "list", Kind: "normal", ContentHash: "input", ExpectedHash: "expected"}}
	report := regression.Report{Samples: []regression.SampleResult{{ID: "a", Revision: 2, StepID: "step", Stage: "list", Kind: "normal", ContentHash: "input", ExpectedHash: "expected"}}}
	if !sameRegressionBatch(report, current) {
		t.Fatal("unchanged batch rejected")
	}
	for _, change := range []func(*regression.SampleResult){func(s *regression.SampleResult) { s.Revision++ }, func(s *regression.SampleResult) { s.ExpectedHash = "edited" }, func(s *regression.SampleResult) { s.ContentHash = "different" }, func(s *regression.SampleResult) { s.StepID = "other" }, func(s *regression.SampleResult) { s.Kind = "missing_field" }} {
		copy := report
		copy.Samples = append([]regression.SampleResult{}, report.Samples...)
		change(&copy.Samples[0])
		if sameRegressionBatch(copy, current) {
			t.Fatal("stale evidence accepted")
		}
	}
	report.Samples = append(report.Samples, report.Samples[0])
	if sameRegressionBatch(report, current) {
		t.Fatal("added/duplicated sample accepted")
	}
}
