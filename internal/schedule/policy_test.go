package schedule

import (
	"testing"
	"time"

	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/trial"
)

func TestScheduleTimezoneAndCron(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	next, err := Next("0 9 * * *", "Asia/Shanghai", now)
	if err != nil || !next.Equal(time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("next=%v err=%v", next, err)
	}
	next, err = Next("0 9 * * *", "Asia/Shanghai", next)
	if err != nil || !next.Equal(time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("strict next=%v err=%v", next, err)
	}
	for _, item := range [][2]string{{"@every 1s", "UTC"}, {"* * * * * *", "UTC"}, {"CRON_TZ=UTC * * * * *", "UTC"}, {"0 0 31 2 *", "UTC"}, {"* * * * *", "Local"}, {"* * * * *", "No/Zone"}} {
		if _, err = Next(item[0], item[1], now); err == nil {
			t.Errorf("accepted invalid %v", item)
		}
	}
	// A nonexistent spring-forward wall time is skipped rather than shifted into another hour.
	before := time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)
	next, err = Next("30 2 * * *", "America/New_York", before)
	if err != nil || !next.Equal(time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("DST next=%v err=%v", next, err)
	}
}
func TestScheduleAPIOverrides(t *testing.T) {
	input := runmodel.Input{VersionID: "f4f9cc3e-0057-4c57-b47a-441793a328d0", Confirmed: true, Origins: []string{"https://example.org"}, Budget: trial.Budget{Seconds: 60, Pages: 5, Records: 20, Details: 2}}
	reduced, err := Reduce(input, &trial.Budget{Seconds: 30, Pages: 2, Records: 10, Details: 0})
	if err != nil || reduced.VersionID != input.VersionID || reduced.Origins[0] != input.Origins[0] || input.Budget.Seconds != 60 {
		t.Fatalf("invalid bounded override %+v %v", reduced, err)
	}
	for _, b := range []trial.Budget{{Seconds: 61, Pages: 5, Records: 20, Details: 2}, {Seconds: 60, Pages: 6, Records: 20, Details: 2}, {Seconds: 60, Pages: 5, Records: 21, Details: 2}, {Seconds: 60, Pages: 5, Records: 20, Details: 3}, {Seconds: 0, Pages: 1, Records: 1}} {
		if _, err = Reduce(input, &b); err == nil {
			t.Errorf("accepted bad budget %+v", b)
		}
	}
}
func TestScheduleBoundedOverlap(t *testing.T) {
	for _, c := range []struct {
		p    string
		a, q int64
		want string
	}{{"skip", 0, 0, "created"}, {"skip", 1, 0, "skipped_overlap"}, {"queue", 1, 0, "queued"}, {"queue", 2, 1, "skipped_overlap"}} {
		if got := OverlapDecision(c.p, c.a, c.q); got != c.want {
			t.Errorf("%+v => %s", c, got)
		}
	}
}

func TestScheduleBudgetShape(t *testing.T) {
	for _, raw := range []string{"", `null`} {
		b, err := DecodeBudget([]byte(raw))
		if err != nil || b != nil {
			t.Fatal("default budget rejected")
		}
	}
	b, err := DecodeBudget([]byte(`{"seconds":30,"pages":2,"records":10,"details":0}`))
	if err != nil || b.Details != 0 {
		t.Fatal("explicit zero rejected", err)
	}
	for _, raw := range []string{`{}`, `[]`, `{"seconds":30,"pages":2,"records":10}`, `{"seconds":30,"pages":2,"records":10,"details":null}`, `{"seconds":30,"pages":2,"records":10,"details":0,"extra":1}`, `{"seconds":30.5,"pages":2,"records":10,"details":0}`} {
		if _, err = DecodeBudget([]byte(raw)); err == nil {
			t.Error("accepted invalid shape", raw)
		}
	}
}
