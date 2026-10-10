package v22_run_repo

import (
	"context"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/publication"
	runmodel "github.com/nekoimi/scrapio/internal/run"
)

func Retry(ctx context.Context, owner int64, parentID, key string, request runmodel.RetryInput, caps publication.Capabilities) (*table.V22Run, error) {
	if err := request.Validate(); err != nil {
		return nil, invalid(err.Error())
	}
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	parent := new(table.V22Run)
	has, err := s.Where("id=? AND owner_id=?", parentID, owner).Get(parent)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	var input runmodel.Input
	if runmodel.Decode(parent.Input, &input) != nil || input.VersionID != parent.VersionId {
		return nil, ErrConflict
	}
	row, err := createTx(s, owner, parent.CollectorId, key, input, &caps, "manual", "", parent)
	if err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

type Filter struct {
	Status    string
	Source    string
	Committed bool
	Since     string
	Until     string
}

func (f Filter) Validate() error {
	if f.Status != "" && f.Status != "queued" && f.Status != "running" && f.Status != "succeeded" && f.Status != "limited" && f.Status != "partial" && f.Status != "failed" && f.Status != "cancelled" {
		return invalid("invalid run status")
	}
	if f.Source != "" && f.Source != "manual" && f.Source != "schedule" && f.Source != "api" && f.Source != "retry" {
		return invalid("invalid trigger source")
	}
	if f.Since != "" || f.Until != "" {
		start, e1 := time.Parse(time.RFC3339Nano, f.Since)
		end, e2 := time.Parse(time.RFC3339Nano, f.Until)
		if e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 31*24*time.Hour {
			return invalid("paired RFC3339 completion window must be positive and at most 31 days")
		}
	}
	return nil
}

func FilteredList(ctx context.Context, owner, collector int64, cursor string, limit int, filter Filter) ([]table.V22Run, bool, error) {
	if err := filter.Validate(); err != nil {
		return nil, false, err
	}
	if limit < 1 || limit > 50 {
		return nil, false, invalid("limit must be 1..50")
	}
	s := db.Instance().Context(ctx).Where("owner_id=?", owner)
	if collector > 0 {
		s.And("collector_id=?", collector)
	}
	if filter.Status != "" {
		s.And("status=?", filter.Status)
	}
	if filter.Source == "retry" {
		s.And("retry_of IS NOT NULL")
	} else if filter.Source != "" {
		s.And("trigger_source=? AND retry_of IS NULL", filter.Source)
	}
	if filter.Committed {
		s.And("summary->>'committed'='true'")
	}
	if filter.Since != "" {
		start, _ := time.Parse(time.RFC3339Nano, filter.Since)
		end, _ := time.Parse(time.RFC3339Nano, filter.Until)
		s.And("finished_at>=? AND finished_at<=?", start, end)
	}
	if cursor != "" {
		a, err := Get(ctx, owner, cursor, "")
		if err != nil {
			return nil, false, err
		}
		if collector > 0 && a.CollectorId != collector || filter.Status != "" && a.Status != filter.Status || filter.Source == "retry" && a.RetryOf == nil || filter.Source != "" && filter.Source != "retry" && (a.TriggerSource != filter.Source || a.RetryOf != nil) {
			return nil, false, ErrNotFound
		}
		if filter.Committed {
			var summary struct {
				Committed bool `json:"committed"`
			}
			if runmodel.Decode(a.Summary, &summary) != nil || !summary.Committed {
				return nil, false, ErrNotFound
			}
		}
		if filter.Since != "" {
			start, _ := time.Parse(time.RFC3339Nano, filter.Since)
			end, _ := time.Parse(time.RFC3339Nano, filter.Until)
			if a.FinishedAt == nil || a.FinishedAt.Before(start) || a.FinishedAt.After(end) {
				return nil, false, ErrNotFound
			}
		}
		s.And("(created_at,id)<(?,?)", a.CreatedAt, a.Id)
	}
	rows := []table.V22Run{}
	err := s.Omit("definition", "output_schema").Desc("created_at", "id").Limit(limit + 1).Find(&rows)
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, more, err
}

// Counts cover the owner's complete retained history, not just the loaded page.
func Statistics(ctx context.Context, owner, collector int64) (map[string]int64, error) {
	args := []any{"SELECT status,count(*)::text AS count FROM v22_runs WHERE owner_id=?", owner}
	if collector > 0 {
		args[0] = args[0].(string) + " AND collector_id=?"
		args = append(args, collector)
	}
	args[0] = args[0].(string) + " GROUP BY status"
	rows, err := db.Instance().Context(ctx).QueryString(args...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{"total": 0, "queued": 0, "running": 0, "succeeded": 0, "limited": 0, "partial": 0, "failed": 0, "cancelled": 0}
	for _, row := range rows {
		n, e := strconv.ParseInt(row["count"], 10, 64)
		if e != nil {
			return nil, e
		}
		counts[row["status"]] = n
		counts["total"] += n
	}
	return counts, nil
}
