package v22_repair_repo

import (
	"github.com/nekoimi/scrapio/internal/repair"
	"testing"
)

func TestD01IdempotencyBindsChoiceRevisionAndSource(t *testing.T) {
	i := repair.Input{RunID: "one", Mode: "continue", ExpectedRevision: 2, Confirmed: true}
	base := fingerprint(1, i)
	for _, change := range []func(*repair.Input){func(v *repair.Input) { v.Mode = "fork" }, func(v *repair.Input) { v.ExpectedRevision++ }, func(v *repair.Input) { v.DocumentID = "other" }, func(v *repair.Input) { v.RunID = "two" }} {
		next := i
		change(&next)
		if fingerprint(1, next) == base {
			t.Fatal("different request reused original repair")
		}
	}
	if fingerprint(2, i) == base || fingerprint(1, i) != base {
		t.Fatal("collector scope or retry unstable")
	}
}
