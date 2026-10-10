package v22_quality_repo

import (
	"context"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/quality"
	"strconv"
	"time"
)

// A rotating page prevents the first few collectors monopolizing schedule checks.
func ScanSchedules(ctx context.Context, after int64) (int64, error) {
	s := session(ctx)
	defer s.Close()
	rows, e := s.QueryString(`SELECT s.collector_id,s.owner_id FROM v22_schedules s JOIN v22_collectors c ON c.id=s.collector_id AND c.owner_id=s.owner_id WHERE c.status<>'archived' AND c.id>? ORDER BY c.id LIMIT 10`, after)
	if e != nil {
		return after, e
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, row := range rows {
		id, _ := strconv.ParseInt(row["collector_id"], 10, 64)
		owner, _ := strconv.ParseInt(row["owner_id"], 10, 64)
		if e = scanSchedule(ctx, owner, id); e != nil {
			return after, e
		}
		after = id
	}
	return after, nil
}
func scanSchedule(ctx context.Context, owner, id int64) error {
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	if e := guard(s, owner, id); e != nil {
		return e
	}
	p, e := policy(s, owner, id)
	if e != nil {
		return e
	}
	if !p.Policy.Enabled {
		return s.Commit()
	}
	r := new(table.V22Schedule)
	has, e := s.Where("owner_id=? AND collector_id=?", owner, id).Get(r)
	if e != nil {
		return e
	}
	if !has {
		return s.Commit()
	}
	code := ""
	switch r.LastDecision {
	case "disabled_invalid", "disabled_unavailable", "skipped_capacity":
		code = "SCHEDULE_BLOCKED"
	}
	if code == "" && r.Enabled && (r.NextAt == nil || r.NextAt.Before(time.Now().Add(-time.Duration(p.Policy.ScheduleGraceMinutes)*time.Minute))) {
		code = "SCHEDULE_STALLED"
	}
	if code != "" {
		prior := new(table.V22QualityIssue)
		has, e = s.Where("owner_id=? AND collector_id=? AND code=?", owner, id, code).Get(prior)
		if e != nil {
			return e
		}
		if !has || prior.Status != "open" {
			if e = upsert(s, owner, id, quality.Finding{Code: code, Category: "schedule", Message: "运行计划阻断或超过到期宽限，请核对调度证据"}, quality.Facts{Reason: r.LastDecision, FinishedAt: time.Now(), FieldKeys: []string{}}); e != nil {
				return e
			}
		}
	}
	return s.Commit()
}
