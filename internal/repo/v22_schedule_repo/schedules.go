package v22_schedule_repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_run_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/schedule"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
)

func invalid(err error) error { return errors.Join(v22_run_repo.ErrInvalid, err) }
func collector(s *xorm.Session, owner, id int64) error {
	c := new(table.V22Collector)
	has, err := s.Where("owner_id=? AND id=? AND status<>'archived'", owner, id).Get(c)
	if err != nil {
		return err
	}
	if !has {
		return v22_run_repo.ErrNotFound
	}
	return nil
}

// ValidateBinding checks the immutable version, destination and permitted origins without target access.
func ValidateBinding(s *xorm.Session, owner, id int64, input runmodel.Input) error {
	if err := input.Validate(); err != nil {
		return invalid(err)
	}
	if _, err := s.QueryString("SELECT id FROM v22_collectors WHERE owner_id=? AND id=? FOR UPDATE", owner, id); err != nil {
		return err
	}
	if err := collector(s, owner, id); err != nil {
		return err
	}
	v := new(table.V22Version)
	has, err := s.Where("owner_id=? AND collector_id=? AND id=?", owner, id, input.VersionID).Get(v)
	if err != nil {
		return err
	}
	if !has {
		return v22_run_repo.ErrNotFound
	}
	p, schema, err := v22_run_repo.VersionPlan(v)
	if err != nil {
		return err
	}
	if err = trial.ValidateScope(p, input.Trial(v.CollectorRevision)); err != nil {
		return invalid(err)
	}
	rows, err := s.QueryString("SELECT id FROM v22_data_tables WHERE owner_id=? AND id=? AND schema_hash=? AND schema_version=? FOR SHARE", owner, p.Output.TableID, output.SchemaHash(schema), p.Output.SchemaVersion)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return v22_run_repo.ErrConflict
	}
	return nil
}
func Get(ctx context.Context, owner, id int64) (*table.V22Schedule, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := collector(s, owner, id); err != nil {
		return nil, err
	}
	row := new(table.V22Schedule)
	has, err := s.Where("owner_id=? AND collector_id=?", owner, id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return row, nil
}
func journal(s *xorm.Session, row *table.V22Schedule, decision string, due *time.Time, runID string, coalesced bool) error {
	_, err := s.Exec("INSERT INTO v22_schedule_events(collector_id,revision,decision,due_at,run_id,coalesced) VALUES(?,?,?,?,?,?)", row.CollectorId, row.Revision, decision, due, runID, coalesced)
	if err != nil {
		return err
	}
	_, err = s.Exec("DELETE FROM v22_schedule_events WHERE collector_id=? AND id NOT IN (SELECT id FROM v22_schedule_events WHERE collector_id=? ORDER BY id DESC LIMIT 100)", row.CollectorId, row.CollectorId)
	return err
}
func Save(ctx context.Context, owner, id int64, c schedule.Config) (*table.V22Schedule, error) {
	if err := c.Validate(time.Now()); err != nil {
		return nil, invalid(err)
	}
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, owner, "schedule.save", stringID(id)); err != nil {
		return nil, err
	}
	if _, err := s.QueryString("SELECT collector_id FROM v22_schedules WHERE owner_id=? AND collector_id=? FOR UPDATE", owner, id); err != nil {
		return nil, err
	}
	row := new(table.V22Schedule)
	has, err := s.Where("owner_id=? AND collector_id=?", owner, id).Get(row)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(c.Input)
	if has && row.Revision != c.ExpectedRevision {
		var old runmodel.Input
		_ = runmodel.Decode(row.Input, &old)
		oldJSON, _ := json.Marshal(old)
		// Retrying a lost response is safe only if the entire requested configuration is still present.
		if row.Revision == c.ExpectedRevision+1 && row.Enabled == c.Enabled && row.Cron == c.Cron && row.Timezone == c.Timezone && row.Overlap == c.Overlap && string(oldJSON) == string(raw) {
			return row, nil
		}
		return nil, v22_run_repo.ErrConflict
	}
	if !has && c.ExpectedRevision != 0 {
		return nil, v22_run_repo.ErrConflict
	}
	var existing runmodel.Input
	_ = runmodel.Decode(row.Input, &existing)
	existingJSON, _ := json.Marshal(existing)
	if !c.Enabled && has && string(existingJSON) == string(raw) {
		// Pausing an existing range must remain possible after a destination becomes incompatible.
		if err = collector(s, owner, id); err != nil {
			return nil, err
		}
	} else if err = ValidateBinding(s, owner, id, c.Input); err != nil {
		return nil, err
	}
	row.CollectorId = id
	row.OwnerId = owner
	row.Revision++
	row.Enabled = c.Enabled
	row.Cron = c.Cron
	row.Timezone = c.Timezone
	row.Overlap = c.Overlap
	row.Input = string(raw)
	row.NextAt = nil
	row.UpdatedAt = time.Now()
	row.LastDecision = "saved_paused"
	row.LastRunId = ""
	if c.Enabled {
		next, _ := schedule.Next(c.Cron, c.Timezone, row.UpdatedAt)
		row.NextAt = &next
		row.LastDecision = "saved_enabled"
	}
	if has {
		_, err = s.Where("collector_id=? AND owner_id=?", id, owner).AllCols().Update(row)
	} else {
		_, err = s.InsertOne(row)
	}
	if err != nil {
		return nil, err
	}
	if err = journal(s, row, row.LastDecision, nil, "", false); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Events(ctx context.Context, owner, id int64) ([]map[string]string, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := collector(s, owner, id); err != nil {
		return nil, err
	}
	return s.QueryString("SELECT id,revision,decision,due_at,run_id,coalesced,created_at FROM v22_schedule_events WHERE collector_id=? ORDER BY id DESC LIMIT 100", id)
}

// Tick atomically advances one due slot and either inserts a run or records why it was skipped.
// Missed slots coalesce into at most one request; no crash window can duplicate a run.
func Tick(ctx context.Context) (bool, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return false, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT collector_id FROM v22_schedules WHERE enabled AND next_at<=clock_timestamp() ORDER BY next_at,collector_id FOR UPDATE SKIP LOCKED LIMIT 1")
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := new(table.V22Schedule)
	has, err := s.Where("collector_id=?", rows[0]["collector_id"]).Get(row)
	if err != nil {
		return false, err
	}
	if !has {
		return false, nil
	}
	clock, err := s.QueryString("SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"') AS now")
	if err != nil {
		return false, err
	}
	now, err := time.Parse(time.RFC3339Nano, clock[0]["now"])
	if err != nil {
		return false, err
	}
	due := row.NextAt
	next, err := schedule.Next(row.Cron, row.Timezone, now)
	if err != nil {
		return false, err
	}
	following, _ := schedule.Next(row.Cron, row.Timezone, *due)
	coalesced := !following.After(now)
	var input runmodel.Input
	if err = runmodel.Decode(row.Input, &input); err != nil {
		return false, err
	}
	// Savepoint removes all partial run inserts on a rejected trigger, while preserving the decision journal.
	if _, err = s.Exec("SAVEPOINT schedule_run"); err != nil {
		return false, err
	}
	key := "schedule:" + stringID(row.CollectorId) + ":" + stringID(int64(row.Revision)) + ":" + due.UTC().Format(time.RFC3339Nano)
	run, createErr := v22_run_repo.CreateTx(s, row.OwnerId, row.CollectorId, key, input, nil, "schedule", row.Overlap)
	decision, runID := "created", ""
	if createErr != nil {
		if _, err = s.Exec("ROLLBACK TO SAVEPOINT schedule_run"); err != nil {
			return false, err
		}
		switch {
		case errors.Is(createErr, v22_run_repo.ErrOverlap):
			decision = "skipped_overlap"
		case errors.Is(createErr, v22_run_repo.ErrCapacity):
			decision = "skipped_capacity"
		case errors.Is(createErr, v22_run_repo.ErrNotFound):
			decision = "disabled_unavailable"
			row.Enabled = false
		case errors.Is(createErr, v22_run_repo.ErrInvalid), errors.Is(createErr, v22_run_repo.ErrConflict):
			decision = "disabled_invalid"
			row.Enabled = false
		default:
			return false, createErr
		}
	} else {
		runID = run.Id
		if row.Overlap == "queue" {
			decision = "accepted_queue"
		}
	}
	row.NextAt = &next
	if !row.Enabled {
		row.NextAt = nil
	}
	row.LastDecision = decision
	row.LastRunId = runID
	if _, err = s.Where("collector_id=?", row.CollectorId).Cols("enabled", "next_at", "last_decision", "last_run_id").Update(row); err != nil {
		return false, err
	}
	if err = journal(s, row, decision, due, runID, coalesced); err != nil {
		return false, err
	}
	return true, s.Commit()
}
