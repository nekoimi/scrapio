package run

import "testing"

func TestC04RetryRequiresExplicitFullScope(t *testing.T) {
	for _, i := range []RetryInput{{}, {Scope: "full_run"}, {Scope: "failed_pages", Confirmed: true}, {Scope: "resume", Confirmed: true}} {
		if i.Validate() == nil {
			t.Fatalf("unsafe scope accepted: %+v", i)
		}
	}
	if (RetryInput{Scope: "full_run", Confirmed: true}).Validate() != nil {
		t.Fatal("confirmed full retry rejected")
	}
	for _, status := range []string{"queued", "running", "unknown"} {
		if c := RunControls(status, false); c.Retry || len(c.Scopes) > 0 {
			t.Fatalf("nonterminal or unknown status retry offered: %+v", c)
		}
	}
	if RunControls("running", true).Cancel {
		t.Fatal("duplicate cancellation offered")
	}
	for _, status := range []string{"succeeded", "limited", "partial", "failed", "cancelled"} {
		if c := RunControls(status, false); !c.Retry || c.Cancel || len(c.Scopes) != 1 || c.Scopes[0] != "full_run" {
			t.Fatalf("terminal controls incorrect: %+v", c)
		}
	}
}
