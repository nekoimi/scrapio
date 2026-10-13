package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
)

func TestCOPublicationRequiresMatchingLiveEvidence(t *testing.T) {
	d := collector.Definition{DefinitionVersion: "collector.v1"}
	p := TestPayload{Revision: 4, Definition: d}
	result := collector.Result{Status: "succeeded", Mode: "live", DefinitionHash: collector.Hash(d), Pages: []collector.PageEvidence{{EmptyVerified: true}}}
	op := &Operation{ID: "e", Operation: "testCollector", Status: "succeeded", ResourceID: "c", Payload: collector.JSON(p), Result: collector.JSON(result)}
	if _, e := CheckEvidence(op, p); e != nil {
		t.Fatal(e)
	}
	p.Revision = 5
	if _, e := CheckEvidence(op, p); e == nil {
		t.Fatal("stale revision accepted")
	}
	p.Revision = 4
	result.Mode = "offline"
	op.Result = collector.JSON(result)
	if _, e := CheckEvidence(op, p); e == nil {
		t.Fatal("offline publish accepted")
	}
	result.Mode = "live"
	result.Status = "awaiting_confirmation"
	op.Result = collector.JSON(result)
	if _, e := CheckEvidence(op, p); e == nil {
		t.Fatal("unknown action publish accepted")
	}
	result.Status = "limited"
	result.Reason = "PAGINATION_BUDGET"
	op.Result = collector.JSON(result)
	view, e := CheckEvidence(op, p)
	if e != nil || len(view["warnings"].([]string)) == 0 {
		t.Fatal(view, e)
	}
}
func TestCORegressionExplicitMismatch(t *testing.T) {
	actual := []collector.Candidate{{Valid: true, Values: map[string]any{"id": "1"}}}
	if Compare(actual, []map[string]any{{"id": "1"}})["status"] != "matched" {
		t.Fatal("expected match")
	}
	if Compare(actual, []map[string]any{{"id": "2"}})["status"] != "different" {
		t.Fatal("expected difference")
	}
}
