package v22_trial_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/sample"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
)

var ErrNotFound = errors.New("trial or owned input not found")
var ErrConflict = errors.New("trial request or schema changed")
var ErrInvalid = errors.New("invalid trial configuration")

func invalid(err error) error     { return fmt.Errorf("%w: %s", ErrInvalid, err) }
func Terminal(status string) bool { return status != "queued" && status != "running" }
func Create(ownerID, collectorID int64, key string, input trial.Input) (*table.V22Trial, error) {
	if err := input.Validate(); err != nil {
		return nil, invalid(err)
	}
	if key == "" || len([]rune(key)) > 128 {
		return nil, invalid(errors.New("Idempotency-Key required, maximum 128 characters"))
	}
	raw, _ := json.Marshal([]any{collectorID, input})
	fingerprint := capture.Hash(raw)
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "trial.create", key); err != nil {
		return nil, err
	}
	prior := new(table.V22Trial)
	has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return prior, nil
	}
	if err := idempotency.Lock(s, ownerID, "trial.capacity", "owner"); err != nil {
		return nil, err
	}
	count, err := s.Where("owner_id=? AND status IN ('queued','running')", ownerID).Count(new(table.V22Trial))
	if err != nil {
		return nil, err
	}
	if count >= 3 {
		return nil, invalid(errors.New("at most three active trials"))
	}
	count, err = s.Where("owner_id=?", ownerID).Count(new(table.V22Trial))
	if err != nil {
		return nil, err
	}
	if count >= 100 {
		return nil, invalid(errors.New("trial history limit (100), remove completed trials before creating more"))
	}
	if _, err = s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", collectorID, ownerID); err != nil {
		return nil, err
	}
	collector := new(table.V22Collector)
	has, err = s.Where("id=? AND owner_id=?", collectorID, ownerID).Get(collector)
	if err != nil {
		return nil, err
	}
	if !has || collector.Status == "archived" {
		return nil, ErrNotFound
	}
	if collector.Revision != input.ExpectedRevision {
		return nil, &v22_collector_repo.RevisionConflict{Latest: collector}
	}
	plan, err := trial.Compile([]byte(collector.Definition), collector.EntryType)
	if err != nil {
		return nil, invalid(err)
	}
	if err = trial.ValidateScope(plan, input); err != nil {
		return nil, invalid(err)
	}
	schemaRow := new(table.V22DataTable)
	id, _ := strconv.ParseInt(plan.Output.TableID, 10, 64)
	has, err = s.Where("id=? AND owner_id=?", id, ownerID).Get(schemaRow)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if schemaRow.SchemaVersion != plan.Output.SchemaVersion || schemaRow.SchemaHash != plan.Output.SchemaHash {
		return nil, ErrConflict
	}
	schema, err := output.DecodeSchema([]byte(schemaRow.Schema))
	if err != nil {
		return nil, err
	}
	if output.SchemaHash(schema) != schemaRow.SchemaHash {
		return nil, ErrConflict
	}
	for _, step := range plan.Steps {
		if step.ID == plan.Output.StepID && !output.CheckCompatibility(schema, plan.Output.Mapping, output.SourceFields(step.Plan, plan.Output.Stage)).Compatible {
			return nil, invalid(errors.New("saved rules incompatible with output table; check output again"))
		}
	}
	fixed := map[string]trial.FixedDocument{}
	for _, ref := range input.Inputs {
		stepFound := false
		for _, step := range plan.Steps {
			if step.ID == ref.StepID && (step.Kind == "record_set" || step.Kind == "json_records") {
				stepFound = true
				if ref.Stage == "detail" && (step.Kind != "record_set" || step.Plan.HTML.Detail == nil) {
					return nil, invalid(errors.New("offline detail input requires a detail path"))
				}
			}
		}
		if !stepFound {
			return nil, invalid(errors.New("input refers to missing extraction step"))
		}
		snapshot := new(table.V22Capture)
		has, err = s.Where("id=? AND owner_id=? AND collector_id=?", ref.CaptureID, ownerID, collectorID).Get(snapshot)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, ErrNotFound
		}
		if snapshot.Status != "succeeded" || snapshot.ContentHash != capture.Hash([]byte(snapshot.Content)) {
			return nil, invalid(errors.New("intact successful snapshot required"))
		}
		fixed[ref.StepID+":"+ref.Stage] = trial.FixedDocument{CaptureID: snapshot.Id, Content: snapshot.Content, Hash: snapshot.ContentHash, Format: snapshot.Format, URL: snapshot.FinalURL, BaseURL: snapshot.BaseURL}
	}
	if input.Mode == "offline" {
		if plan.Output.Stage == "detail" {
			if _, has := fixed[plan.Output.StepID+":detail"]; !has {
				return nil, invalid(errors.New("detail output requires a fixed detail input"))
			}
		}
		for _, step := range plan.Steps {
			if step.Kind == "record_set" || step.Kind == "json_records" {
				if _, has := fixed[step.ID+":list"]; !has {
					return nil, invalid(fmt.Errorf("step %s requires a fixed list input", step.ID))
				}
			}
		}
	}
	config, _ := json.Marshal(input)
	inputs, _ := json.Marshal(fixed)
	row := &table.V22Trial{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, CollectorRevision: collector.Revision, Definition: collector.Definition, DefinitionHash: capture.Hash(sample.CanonicalJSON([]byte(collector.Definition))), EntryType: collector.EntryType, Input: string(config), FixedInputs: string(inputs), OutputSchema: schemaRow.Schema, Status: "queued", Summary: "{}", EventSeq: 1, IdempotencyKey: key, Fingerprint: fingerprint, CreatedAt: time.Now()}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("INSERT INTO v22_trial_events(trial_id,sequence,payload) VALUES(?,1,?::jsonb)", row.Id, `{"status":"queued"}`); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Get(ownerID int64, id, key string) (*table.V22Trial, error) {
	row := new(table.V22Trial)
	s := db.Instance().Where("owner_id=?", ownerID)
	if key != "" {
		s.And("idempotency_key=?", key)
	} else {
		s.And("id=?", id)
	}
	has, err := s.Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func List(ownerID, collectorID int64) ([]table.V22Trial, error) {
	rows := []table.V22Trial{}
	err := db.Instance().Where("owner_id=? AND collector_id=?", ownerID, collectorID).Desc("created_at", "id").Limit(20).Find(&rows)
	return rows, err
}
func lock(s *xorm.Session, id string) (*table.V22Trial, error) {
	if _, err := s.QueryString("SELECT id FROM v22_trials WHERE id=? FOR UPDATE", id); err != nil {
		return nil, err
	}
	row := new(table.V22Trial)
	has, err := s.ID(id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}
func event(s *xorm.Session, row *table.V22Trial, payload any) error {
	data, _ := json.Marshal(payload)
	row.EventSeq++
	if _, err := s.Exec("INSERT INTO v22_trial_events(trial_id,sequence,payload) VALUES(?,?,?::jsonb)", row.Id, row.EventSeq, string(data)); err != nil {
		return err
	}
	_, err := s.ID(row.Id).Cols("event_seq", "status", "cancel_requested", "current_step", "current_stage", "summary", "finished_at").Update(row)
	return err
}
func Cancel(ownerID int64, id string) (*table.V22Trial, error) {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	row, err := lock(s, id)
	if err != nil {
		return nil, err
	}
	if row.OwnerId != ownerID {
		return nil, ErrNotFound
	}
	if !Terminal(row.Status) && !row.CancelRequested {
		row.CancelRequested = true
		if row.Status == "queued" {
			row.Status = "cancelled"
			now := time.Now()
			row.FinishedAt = &now
			row.Summary = `{"status":"cancelled","stop_reason":"CANCEL_REQUESTED","dry_run":true,"network_accessed":false}`
		}
		if err = event(s, row, trial.Event{Status: row.Status, Code: "CANCEL_REQUESTED"}); err != nil {
			return nil, err
		}
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Delete(ownerID int64, id string) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	row, err := lock(s, id)
	if err != nil {
		return err
	}
	if row.OwnerId != ownerID {
		return ErrNotFound
	}
	if !Terminal(row.Status) {
		return ErrConflict
	}
	if _, err = s.ID(id).Delete(new(table.V22Trial)); err != nil {
		return err
	}
	return s.Commit()
}
func Claim() (*table.V22Trial, error) {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_trials WHERE status='queued' AND NOT cancel_requested ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED")
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := new(table.V22Trial)
	if _, err = s.ID(rows[0]["id"]).Get(row); err != nil {
		return nil, err
	}
	row.Status = "running"
	now := time.Now()
	row.StartedAt = &now
	if _, err = s.ID(row.Id).Cols("started_at").Update(row); err != nil {
		return nil, err
	}
	if err = event(s, row, trial.Event{Status: "running"}); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Progress(id string, e trial.Event, document *trial.Document) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	row, err := lock(s, id)
	if err != nil {
		return err
	}
	if row.CancelRequested || row.Status != "running" {
		return context.Canceled
	}
	row.CurrentStep = e.StepID
	row.CurrentStage = e.Stage
	if document != nil {
		document.ID = uuid.NewString()
		data, _ := json.Marshal(document)
		if _, err = s.InsertOne(&table.V22TrialDocument{Id: document.ID, TrialId: id, Sequence: row.EventSeq + 1, Content: document.Content, Result: string(data)}); err != nil {
			return err
		}
	}
	if err = event(s, row, e); err != nil {
		return err
	}
	return s.Commit()
}
func Finish(id string, summary trial.Summary) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	row, err := lock(s, id)
	if err != nil {
		return err
	}
	if Terminal(row.Status) {
		return nil
	}
	if row.CancelRequested {
		summary.Status = "cancelled"
		summary.Reason = "CANCEL_REQUESTED"
	}
	data, _ := json.Marshal(summary)
	row.Summary = string(data)
	row.Status = summary.Status
	now := time.Now()
	row.FinishedAt = &now
	if err = event(s, row, trial.Event{StepID: summary.FailedStep, Stage: summary.FailedStage, Status: row.Status, Code: summary.Reason}); err != nil {
		return err
	}
	return s.Commit()
}
func SetSession(id, sessionID string) error {
	_, err := db.Instance().ID(id).Cols("session_id").Update(&table.V22Trial{SessionId: sessionID})
	return err
}
func Interrupted() ([]table.V22Trial, error) {
	rows := []table.V22Trial{}
	err := db.Instance().Where("status='running'").Find(&rows)
	return rows, err
}
func Results(id string, after int64, limit int) ([]table.V22TrialDocument, error) {
	rows := []table.V22TrialDocument{}
	err := db.Instance().Where("trial_id=? AND sequence>?", id, after).Asc("sequence").Limit(limit).Find(&rows)
	return rows, err
}
func Events(id string, after int64) ([]table.V22TrialEvent, error) {
	rows := []table.V22TrialEvent{}
	err := db.Instance().Where("trial_id=? AND sequence>?", id, after).Asc("sequence").Limit(50).Find(&rows)
	return rows, err
}
func Existing(ctx context.Context, row *table.V22Trial, config output.Config, schema output.Schema, result extraction.Result) (map[string]output.Existing, error) {
	target := new(table.V22DataTable)
	id, _ := strconv.ParseInt(config.TableID, 10, 64)
	has, err := db.Instance().Context(ctx).Where("id=? AND owner_id=?", id, row.OwnerId).Get(target)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if target.SchemaHash != config.SchemaHash || target.SchemaVersion != config.SchemaVersion {
		return nil, ErrConflict
	}
	existing := map[string]output.Existing{}
	for _, record := range result.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prepared, issues := output.Prepare(schema, config.Mapping, record)
		if len(issues) > 0 {
			continue
		}
		if _, seen := existing[prepared.Key]; seen {
			continue
		}
		records, err := db.Instance().Context(ctx).QueryString("SELECT id,revision,record_values::text AS record_values FROM v22_table_records WHERE table_id=? AND canonical_key=?", id, prepared.Key)
		if err != nil {
			return nil, err
		}
		if len(records) > 0 {
			values, err := output.DecodeValues(records[0]["record_values"])
			if err != nil {
				return nil, err
			}
			revision, _ := strconv.Atoi(records[0]["revision"])
			existing[prepared.Key] = output.Existing{RecordID: records[0]["id"], Revision: revision, Values: values}
		}
	}
	return existing, nil
}
