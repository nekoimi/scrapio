// Package schedule defines bounded, version-pinned triggers for v2.2.
package schedule

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	_ "time/tzdata"

	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/trial"
	"github.com/robfig/cron/v3"
)

// DecodeBudget distinguishes omitted fields from an explicit zero detail budget.
func DecodeBudget(raw json.RawMessage) (*trial.Budget, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 4 {
		return nil, errors.New("budget requires seconds, pages, records and details")
	}
	for _, key := range []string{"seconds", "pages", "records", "details"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, errors.New("budget requires four integer values")
		}
	}
	var budget trial.Budget
	if err := json.Unmarshal(raw, &budget); err != nil {
		return nil, errors.New("invalid budget integers")
	}
	return &budget, nil
}

type Config struct {
	ExpectedRevision int            `json:"expected_revision"`
	Enabled          bool           `json:"enabled"`
	Cron             string         `json:"cron"`
	Timezone         string         `json:"timezone"`
	Overlap          string         `json:"overlap"`
	Input            runmodel.Input `json:"input"`
}

func Next(expression, timezone string, after time.Time) (time.Time, error) {
	if len(expression) > 128 || len(timezone) > 64 || len(strings.Fields(expression)) != 5 || strings.Contains(expression, "=") {
		return time.Time{}, errors.New("requires five-field cron and separate IANA timezone")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" || timezone == "Local" {
		return time.Time{}, errors.New("invalid IANA timezone")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	plan, err := parser.Parse(expression)
	if err != nil {
		return time.Time{}, errors.New("invalid five-field cron")
	}
	next := plan.Next(after.In(loc))
	if next.IsZero() {
		return next, errors.New("cron has no next execution")
	}
	return next.UTC(), nil
}
func (c Config) Validate(now time.Time) error {
	if c.ExpectedRevision < 0 || (c.Overlap != "skip" && c.Overlap != "queue") {
		return errors.New("invalid revision or overlap policy")
	}
	if err := c.Input.Validate(); err != nil {
		return err
	}
	_, err := Next(c.Cron, c.Timezone, now)
	return err
}

// API overrides are budget reductions only: no source, scope, version or rule changes.
func Reduce(input runmodel.Input, budget *trial.Budget) (runmodel.Input, error) {
	if budget == nil {
		return input, input.Validate()
	}
	b, max := *budget, input.Budget
	if b.Seconds > max.Seconds || b.Pages > max.Pages || b.Records > max.Records || b.Details > max.Details {
		return input, errors.New("API budget cannot exceed credential budget")
	}
	input.Budget = b
	return input, input.Validate()
}

func OverlapDecision(policy string, active, queued int64) string {
	if active == 0 {
		return "created"
	}
	if policy == "queue" && queued == 0 {
		return "queued"
	}
	return "skipped_overlap"
}
