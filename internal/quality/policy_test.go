package quality

import (
	"strings"
	"testing"
	"time"
)

func good() Facts {
	return Facts{RunID: "r", VersionID: "v", Key: "scope", Status: "succeeded", Network: true, Committed: true, Valid: 20, Pages: 2, FinishedAt: time.Now(), StartedAt: time.Now().Add(-time.Minute)}
}
func TestQualityBaselineAndRecovery(t *testing.T) {
	p := Default()
	f := good()
	r := Evaluate(p, f, nil, 0)
	if !r.RecoveryEligible || len(r.Findings) != 0 || r.BaselineStatus != "not_configured" {
		t.Fatal("unconfigured baseline invented anomaly", r)
	}
	base := good()
	base.Valid = 100
	base.FinishedAt = f.FinishedAt.Add(-time.Hour)
	r = Evaluate(p, f, &base, 0)
	if r.RecoveryEligible || len(r.Findings) != 1 || r.Findings[0].Code != "VALID_RECORDS_DROP" {
		t.Fatal(r)
	}
	base.Key = "other scope"
	r = Evaluate(p, f, &base, 0)
	if r.RecoveryEligible || len(r.Findings) != 0 || r.BaselineStatus != "incomparable" {
		t.Fatal("incomparable evidence used", r)
	}
	for _, mutate := range []func(*Facts){func(f *Facts) { f.Status = "limited" }, func(f *Facts) { f.Network = false }, func(f *Facts) { f.Committed = false }, func(f *Facts) { f.FailedStage = "list" }, func(f *Facts) { f.Status = "cancelled" }} {
		x := f
		mutate(&x)
		if Evaluate(p, x, nil, 0).RecoveryEligible {
			t.Fatal("incomplete/offline evidence proved recovery", x)
		}
	}
	p.Enabled = false
	if Evaluate(p, f, nil, 0).RecoveryEligible {
		t.Fatal("disabled policy resolved issue")
	}
}
func TestQualityFailureAttribution(t *testing.T) {
	f := good()
	f.Status = "failed"
	f.Committed = false
	f.Reason = "BROWSER_RPC_FAILED"
	f.FieldKeys = []string{"title"}
	r := Evaluate(Default(), f, nil, 0)
	if len(r.Findings) != 1 || r.Findings[0].Code != "BROWSER_FAILURE" {
		t.Fatal("browser misclassified as selector", r)
	}
	f.Reason = "VALIDATION_FAILED"
	f.Invalid = 30
	r = Evaluate(Default(), f, nil, 1)
	if r.FailureStreak != 2 || len(r.Findings) < 2 || r.Findings[0].Code != "FIELD_MISMATCH" {
		t.Fatal(r)
	}
	f.Status = "cancelled"
	r = Evaluate(Default(), f, nil, 2)
	if r.FailureStreak != 2 || len(r.Findings) != 0 {
		t.Fatal("intentional cancellation counted as failure", r)
	}
	p := Default()
	p.MinValid = 30
	f = good()
	r = Evaluate(p, f, nil, 0)
	if len(r.Findings) != 1 || r.Findings[0].Code != "VALID_RECORDS_LOW" {
		t.Fatal(r)
	}
}
func TestQualityScopeAllowsSelectorRepair(t *testing.T) {
	def := `{"entry_url":"https://example.com","steps":[{"step_id":"s","type":"record_set","config":{"mode":"repeated","locator":{"expression":"old"},"fields":[{"name":"title"}],"max_records":20}}],"output":{"step_id":"s","stage":"list","check_id":"old","table_id":"1"}}`
	schema := `{"version":1}`
	input := `{"version_id":"old","confirmed":true,"allowed_origins":["https://example.com"],"budget":{"pages":5,"records":50}}`
	a := Comparable(def, schema, input, "web")
	b := Comparable(strings.ReplaceAll(def, "old", "new"), schema, strings.ReplaceAll(input, "old", "new"), "web")
	if a == "" || a != b {
		t.Fatal("selector repair changed scope identity")
	}
	if a == Comparable(def, schema, strings.Replace(input, `"pages":5`, `"pages":1`, 1), "web") {
		t.Fatal("changed budget treated comparable")
	}
	if a == Comparable(def, `{"version":2}`, input, "web") {
		t.Fatal("schema changed without baseline")
	}
}
