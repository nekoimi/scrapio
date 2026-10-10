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
