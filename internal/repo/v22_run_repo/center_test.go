package v22_run_repo

import "testing"

func TestC04FiltersRejectUnknownRuntimeValues(t *testing.T) {
	for _, f := range []Filter{{Status: "all"}, {Source: "draft"}, {Status: "running OR true"}} {
		if f.Validate() == nil {
			t.Fatal("invalid filter accepted", f)
		}
	}
	for _, f := range []Filter{{}, {Status: "partial", Source: "retry"}, {Status: "limited", Source: "schedule"}} {
		if f.Validate() != nil {
			t.Fatal("valid filter rejected", f)
		}
	}
}

func TestC05CompletionWindowIsBoundedAndPaired(t *testing.T) {
	for _, f := range []Filter{{Since: "2026-10-10T00:00:00Z"}, {Since: "bad", Until: "bad"}, {Since: "2026-10-10T00:00:00Z", Until: "2026-10-09T00:00:00Z"}, {Since: "2026-01-01T00:00:00Z", Until: "2026-10-10T00:00:00Z"}} {
		if f.Validate() == nil {
			t.Fatal("invalid completion window accepted", f)
		}
	}
	if err := (Filter{Committed: true, Since: "2026-10-09T00:00:00Z", Until: "2026-10-10T00:00:00Z"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
