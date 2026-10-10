package overview

import "testing"

func TestQualityIssueSurvivesSuccessfulRun(t *testing.T) {
	h := Health{Baseline: "selected", LatestTerminal: &Run{ID: "good", Status: "succeeded", Committed: true}, QualityIssues: []Issue{{ID: "issue", Kind: "quality_issue", Status: "ready", Code: "FIELD_MISMATCH"}}}
	Evaluate(&h)
	if h.Outcome != "needs_attention" || len(h.Issues) != 1 || h.Issues[0].ID != "issue" || h.Baseline != "selected" {
		t.Fatal("successful run hid pending recovery", h)
	}
}

func TestC05HealthUsesCompletedEvidenceWithoutInventingBaseline(t *testing.T) {
	h := Health{LatestTerminal: &Run{ID: "effective", Status: "succeeded", Committed: true, Counts: map[string]int64{"created": 0, "updated": 0, "unchanged": 20}}}
	Evaluate(&h)
	if h.Outcome != "healthy" || len(h.Issues) != 0 || h.Baseline != "not_configured" {
		t.Fatal("zero additions must not be an anomaly", h)
	}
	h.LatestTerminal = &Run{ID: "failed", Status: "partial", Reason: "OUTPUT_VALIDATION_FAILED"}
	h.LatestRun = &Run{ID: "newer", Status: "running"}
	h.ActiveRuns = 1
	Evaluate(&h)
	if h.Outcome != "needs_attention" || len(h.Issues) != 1 || h.Issues[0].RunID != "failed" {
		t.Fatal("new active work hid prior completed failure", h)
	}
	h.LatestTerminal = &Run{ID: "success", Status: "succeeded", Committed: true}
	h.Schedule = &Schedule{LastDecision: "disabled_invalid"}
	Evaluate(&h)
	if len(h.Issues) != 1 || h.Issues[0].Kind != "schedule_blocked" {
		t.Fatal("successful run must not resolve invalid schedule", h)
	}
	h.Schedule = &Schedule{LastDecision: "saved_paused"}
	Evaluate(&h)
	if len(h.Issues) != 0 {
		t.Fatal("superseded failure or intentional pause retained fabricated issue", h)
	}
}
func TestC05CoverageCancellationAndScheduleAreDistinct(t *testing.T) {
	for _, decision := range []string{"skipped_overlap", "accepted_queue", "saved_paused", "saved_enabled", "created"} {
		h := Health{LatestTerminal: &Run{Status: "cancelled"}, Schedule: &Schedule{LastDecision: decision}}
		Evaluate(&h)
		if h.Outcome != "cancelled" || len(h.Issues) > 0 {
			t.Fatal(h)
		}
	}
	h := Health{LatestTerminal: &Run{ID: "limited", Status: "limited", Committed: true, Reason: "LIST_PAGE_LIMIT_REACHED"}}
	Evaluate(&h)
	if len(h.Issues) != 1 || h.Issues[0].Kind != "limited_coverage" || h.Issues[0].Severity != "warning" {
		t.Fatal(h)
	}
	h.Archived = true
	Evaluate(&h)
	if h.Outcome != "archived" || len(h.Issues) > 0 {
		t.Fatal("archived collector enters active issue queue", h)
	}
}
