package v22_run_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/schedule"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
)

var ErrNotFound = errors.New("run or version not found")
var ErrConflict = errors.New("run request, capability or schema changed")
var ErrInvalid = errors.New("invalid run configuration")
var ErrCapacity = errors.New("formal run active or history capacity reached")
var ErrOverlap = errors.New("collector already has an active or queued run")
var ErrLease = errors.New("run cancelled or attempt lease lost")

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }

func Create(ctx context.Context, ownerID, collectorID int64, key string, input runmodel.Input, caps publication.Capabilities) (*table.V22Run, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	row, err := CreateTx(s, ownerID, collectorID, key, input, &caps, "manual", "")
	if err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

// CreateTx shares caller's transaction, so a trigger decision and queued run commit together.
// A nil capability snapshot defers the live capability check to the formal worker.
func CreateTx(s *xorm.Session, ownerID, collectorID int64, key string, input runmodel.Input, caps *publication.Capabilities, source, overlap string) (*table.V22Run, error) {
	if source != "manual" && source != "schedule" && source != "api" {
		return nil, invalid("invalid trigger source")
	}
	if err := input.Validate(); err != nil {
		return nil, invalid(err.Error())
	}
	if key == "" || len([]rune(key)) > 128 {
		return nil, invalid("Idempotency-Key required, maximum 128 characters")
	}
	fingerprint := publication.Hash([]any{collectorID, input})
	if source != "manual" {
		fingerprint = publication.Hash([]any{collectorID, input, source})
	}
	if err := idempotency.Lock(s, ownerID, "run.create", key); err != nil {
		return nil, err
	}
	prior := new(table.V22Run)
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
	if source == "manual" && (strings.HasPrefix(key, "schedule:") || strings.HasPrefix(key, "api:")) {
		return nil, invalid("reserved trigger request key prefix")
	}
	if err = idempotency.Lock(s, ownerID, "run.capacity", "owner"); err != nil {
		return nil, err
	}
	count, err := s.Where("owner_id=? AND status IN ('queued','running')", ownerID).Count(new(table.V22Run))
	if err != nil {
		return nil, err
	}
	if count >= 3 {
		return nil, ErrCapacity
	}
	count, err = s.Where("owner_id=?", ownerID).Count(new(table.V22Run))
	if err != nil {
		return nil, err
	}
	if count >= 1000 {
		return nil, ErrCapacity
	}
	if _, err = s.QueryString("SELECT id FROM v22_collectors WHERE owner_id=? AND id=? FOR UPDATE", ownerID, collectorID); err != nil {
		return nil, err
	}
	collector := new(table.V22Collector)
	has, err = s.Where("owner_id=? AND id=?", ownerID, collectorID).Get(collector)
	if err != nil {
		return nil, err
	}
	if !has || collector.Status == "archived" {
		return nil, ErrNotFound
	}
	if overlap != "" {
		active, err := s.Where("owner_id=? AND collector_id=? AND status IN ('queued','running')", ownerID, collectorID).Count(new(table.V22Run))
		if err != nil {
			return nil, err
		}
		queued, err := s.Where("owner_id=? AND collector_id=? AND status='queued'", ownerID, collectorID).Count(new(table.V22Run))
		if err != nil {
			return nil, err
		}
		if schedule.OverlapDecision(overlap, active, queued) == "skipped_overlap" {
			return nil, ErrOverlap
		}
	}
	v := new(table.V22Version)
	has, err = s.Where("owner_id=? AND collector_id=? AND id=?", ownerID, collectorID, input.VersionID).Get(v)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	plan, schema, err := VersionPlan(v)
	if err != nil {
		return nil, err
	}
	if caps != nil && caps.DefinitionHash != v.DefinitionHash {
		return nil, ErrConflict
	}
	if caps != nil && !publication.CheckCapabilities(plan, *caps).Ready {
		return nil, invalid("published version requires unavailable browser/HTTP/credential capabilities")
	}
	if err = trial.ValidateScope(plan, input.Trial(v.CollectorRevision)); err != nil {
		return nil, invalid(err.Error())
	}
	id, _ := strconv.ParseInt(plan.Output.TableID, 10, 64)
	if _, err = s.QueryString("SELECT id FROM v22_data_tables WHERE id=? AND owner_id=? FOR SHARE", id, ownerID); err != nil {
		return nil, err
	}
	target := new(table.V22DataTable)
	has, err = s.Where("id=? AND owner_id=?", id, ownerID).Get(target)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if target.SchemaHash != plan.Output.SchemaHash || target.SchemaVersion != plan.Output.SchemaVersion || output.SchemaHash(schema) != target.SchemaHash {
		return nil, ErrConflict
	}
	raw, _ := json.Marshal(input)
	row := &table.V22Run{Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collectorID, VersionId: v.Id, VersionNumber: v.Number, CollectorRevision: v.CollectorRevision, Definition: v.Definition, DefinitionHash: v.DefinitionHash, PublicationContract: v.ContractVersion, InterpreterVersion: v.InterpreterVersion, EntryType: v.EntryType, OutputSchema: v.OutputSchema, Input: string(raw), TriggerSource: source, Status: "queued", Summary: "{}", EventSeq: 1, IdempotencyKey: key, Fingerprint: fingerprint, CreatedAt: time.Now()}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	event, _ := json.Marshal(map[string]string{"status": "queued", "trigger_source": source})
	if _, err = s.Exec("INSERT INTO v22_run_events(run_id,sequence,attempt,payload) VALUES(?,1,0,?::jsonb)", row.Id, string(event)); err != nil {
		return nil, err
	}
	return row, nil
}
func VersionPlan(v *table.V22Version) (trial.Plan, output.Schema, error) {
	p, err := trial.Compile([]byte(v.Definition), v.EntryType)
	if err != nil {
		return p, output.Schema{}, invalid("published plan is invalid")
	}
	s, err := output.DecodeSchema([]byte(v.OutputSchema))
	if err != nil {
		return p, s, invalid("published Schema is invalid")
	}
	if v.ContractVersion != publication.ContractVersion || v.InterpreterVersion != extraction.InterpreterVersion || v.DefinitionHash != publication.DefinitionHash(v.Definition) || p.Output.SchemaHash != output.SchemaHash(s) {
		return p, s, ErrConflict
	}
	for _, step := range p.Steps {
		if step.ID == p.Output.StepID && !output.CheckCompatibility(s, p.Output.Mapping, output.SourceFields(step.Plan, p.Output.Stage)).Compatible {
			return p, s, ErrConflict
		}
	}
	return p, s, nil
}
func Get(ctx context.Context, ownerID int64, id, key string) (*table.V22Run, error) {
	s := db.Instance().Context(ctx).Where("owner_id=?", ownerID)
	if key != "" {
		if len([]rune(key)) > 128 {
			return nil, invalid("invalid request key")
		}
		s.And("idempotency_key=?", key)
	} else {
		s.And("id=?", id)
	}
	r := new(table.V22Run)
	has, err := s.Get(r)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
func List(ctx context.Context, ownerID, collectorID int64, cursor string, limit int) ([]table.V22Run, bool, error) {
	if limit < 1 || limit > 50 {
		return nil, false, invalid("limit must be 1..50")
	}
	s := db.Instance().Context(ctx).Where("owner_id=?", ownerID)
	if collectorID > 0 {
		s.And("collector_id=?", collectorID)
	}
	if cursor != "" {
		anchor, err := Get(ctx, ownerID, cursor, "")
		if err != nil {
			return nil, false, err
		}
		if collectorID > 0 && anchor.CollectorId != collectorID {
			return nil, false, ErrNotFound
		}
		s.And("(created_at,id)<(?,?)", anchor.CreatedAt, anchor.Id)
	}
	rows := []table.V22Run{}
	err := s.Omit("definition", "output_schema").Desc("created_at", "id").Limit(limit + 1).Find(&rows)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return rows, more, nil
}
func lock(s *xorm.Session, id string) (*table.V22Run, error) {
	if _, err := s.QueryString("SELECT id FROM v22_runs WHERE id=? FOR UPDATE", id); err != nil {
		return nil, err
	}
	r := new(table.V22Run)
	has, err := s.ID(id).Get(r)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
func fenced(s *xorm.Session, id, token string) (*table.V22Run, error) {
	r, err := lock(s, id)
	if err != nil {
		return nil, err
	}
	// Use database time so fencing does not depend on worker host clock skew.
	rows, err := s.QueryString("SELECT id FROM v22_runs WHERE id=? AND status='running' AND NOT cancel_requested AND lease_token=? AND lease_until>clock_timestamp()", id, token)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrLease
	}
	return r, nil
}
func event(s *xorm.Session, r *table.V22Run, e trial.Event) error {
	r.EventSeq++
	data, _ := json.Marshal(e)
	if _, err := s.Exec("UPDATE v22_runs SET status=?,cancel_requested=?,current_step=?,current_stage=?,event_seq=?,summary=?::jsonb,finished_at=? WHERE id=?", r.Status, r.CancelRequested, r.CurrentStep, r.CurrentStage, r.EventSeq, r.Summary, r.FinishedAt, r.Id); err != nil {
		return err
	}
	_, err := s.Exec("INSERT INTO v22_run_events(run_id,sequence,attempt,payload) VALUES(?,?,?,?::jsonb)", r.Id, r.EventSeq, r.Attempt, string(data))
	return err
}
func Claim(ctx context.Context) (*table.V22Run, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_runs WHERE status='queued' AND NOT cancel_requested ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED")
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	r, err := lock(s, rows[0]["id"])
	if err != nil {
		return nil, err
	}
	r.Attempt++
	r.Status = "running"
	r.LeaseToken = uuid.NewString()
	if _, err = s.Exec("UPDATE v22_runs SET status='running',attempt=?,lease_token=?,lease_until=clock_timestamp()+interval '45 seconds',started_at=clock_timestamp() WHERE id=?", r.Attempt, r.LeaseToken, r.Id); err != nil {
		return nil, err
	}
	if _, err = s.Exec("INSERT INTO v22_run_attempts(run_id,attempt,lease_token,status) VALUES(?,?,?,'running')", r.Id, r.Attempt, r.LeaseToken); err != nil {
		return nil, err
	}
	if _, err = s.ID(r.Id).Get(r); err != nil {
		return nil, err
	}
	if err = event(s, r, trial.Event{Status: "running"}); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return r, nil
}
func Renew(ctx context.Context, id, token string) error {
	res, err := db.Instance().Context(ctx).Exec("UPDATE v22_runs SET lease_until=clock_timestamp()+interval '45 seconds' WHERE id=? AND lease_token=? AND status='running' AND NOT cancel_requested AND lease_until>clock_timestamp()", id, token)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLease
	}
	return nil
}
func Progress(ctx context.Context, id, token string, e trial.Event, doc *trial.Document) error {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	r, err := fenced(s, id, token)
	if err != nil {
		return err
	}
	r.CurrentStep = e.StepID
	r.CurrentStage = e.Stage
	if doc != nil {
		doc.ID = uuid.NewString()
		doc.Result.DryRun = false
		raw, _ := json.Marshal(doc)
		if _, err = s.InsertOne(&table.V22RunDocument{Id: doc.ID, RunId: id, Attempt: r.Attempt, Sequence: r.EventSeq + 1, Content: doc.Content, Result: string(raw)}); err != nil {
			return err
		}
	}
	if e.Checkpoint != nil {
		cp := e.Checkpoint
		if doc != nil || cp.DocumentID == "" || cp.StepID != e.StepID || cp.ListPage < 1 || cp.ListPage > 100 {
			return invalid("checkpoint boundary invalid")
		}
		if cp.State != "page_captured" && cp.State != "page_complete" && cp.State != "action_pending" && cp.State != "awaiting_page" && cp.State != "stopped" {
			return invalid("checkpoint state invalid")
		}
		docs, err := s.QueryString("SELECT id FROM v22_run_documents WHERE id=? AND run_id=? AND attempt=? AND result->>'step_id'=? AND result->>'stage'='list' AND result->>'content_hash'=? AND result->>'list_page'=?", cp.DocumentID, id, r.Attempt, cp.StepID, cp.Hash, strconv.Itoa(cp.ListPage))
		if err != nil {
			return err
		}
		if len(docs) != 1 {
			return invalid("checkpoint document mismatch")
		}
		raw, _ := json.Marshal(cp)
		if len(raw) > 16384 {
			return invalid("checkpoint exceeds budget")
		}
		progress := runmodel.Empty(trial.Summary{Status: "running", Pages: cp.Pages, ListPages: cp.ListPages, Candidates: cp.Candidates, Details: cp.Details, DuplicateRecords: cp.DuplicateRecords, NetworkAccessed: true, Warnings: []string{}, LastCheckpoint: cp})
		progressRaw, _ := json.Marshal(progress)
		r.Summary = string(progressRaw)
		if _, err = s.Exec("INSERT INTO v22_run_checkpoints(id,run_id,attempt,sequence,document_id,state,payload) VALUES(?,?,?,?,?,?,?::jsonb)", uuid.NewString(), id, r.Attempt, r.EventSeq+1, cp.DocumentID, cp.State, string(raw)); err != nil {
			return err
		}
	}
	if err = event(s, r, e); err != nil {
		return err
	}
	return s.Commit()
}
func SetSession(ctx context.Context, id, token, sessionID string) error {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	r, err := fenced(s, id, token)
	if err != nil {
		return err
	}
	if _, err = s.Exec("UPDATE v22_runs SET session_id=? WHERE id=?", sessionID, id); err != nil {
		return err
	}
	if _, err = s.Exec("UPDATE v22_run_attempts SET session_id=? WHERE run_id=? AND attempt=?", sessionID, id, r.Attempt); err != nil {
		return err
	}
	return s.Commit()
}
func Cancel(ctx context.Context, ownerID int64, id string) (*table.V22Run, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	r, err := lock(s, id)
	if err != nil {
		return nil, err
	}
	if r.OwnerId != ownerID {
		return nil, ErrNotFound
	}
	if runmodel.Terminal(r.Status) || r.CancelRequested {
		return r, nil
	}
	r.CancelRequested = true
	if r.Status == "queued" {
		r.Status = "cancelled"
		now := time.Now()
		r.FinishedAt = &now
		raw, _ := json.Marshal(runmodel.Empty(trial.Summary{Status: r.Status, Reason: "CANCEL_REQUESTED", Warnings: []string{}}))
		r.Summary = string(raw)
	}
	if err = event(s, r, trial.Event{Status: r.Status, Code: "CANCEL_REQUESTED"}); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return r, nil
}

// Expired attempts are terminated, never replayed. The caller closes their
// journaled sessions after the fencing/terminal transaction has committed.
func Recover(ctx context.Context) ([]table.V22Run, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	ids, err := s.QueryString("SELECT id FROM v22_runs WHERE status='running' AND lease_until<=clock_timestamp() ORDER BY lease_until LIMIT 10 FOR UPDATE SKIP LOCKED")
	if err != nil {
		return nil, err
	}
	rows := []table.V22Run{}
	for _, id := range ids {
		r, err := lock(s, id["id"])
		if err != nil {
			return nil, err
		}
		r.Status = "failed"
		reason := "LEASE_EXPIRED_OPERATIONS_NOT_REPLAYED"
		if r.CancelRequested {
			r.Status = "cancelled"
			reason = "CANCEL_REQUESTED"
		}
		summary := runmodel.Empty(trial.Summary{Status: r.Status, Reason: reason, FailedStep: r.CurrentStep, FailedStage: r.CurrentStage, NetworkAccessed: true, Warnings: []string{"INTERRUPTED_OPERATIONS_NOT_REPLAYED"}})
		checkpoints, err := s.QueryString("SELECT payload::text FROM v22_run_checkpoints WHERE run_id=? AND attempt=? ORDER BY sequence DESC LIMIT 1", r.Id, r.Attempt)
		if err != nil {
			return nil, err
		}
		if len(checkpoints) > 0 {
			var cp trial.Checkpoint
			if runmodel.Decode(checkpoints[0]["payload"], &cp) != nil {
				return nil, invalid("checkpoint corrupt")
			}
			summary.LastCheckpoint = &cp
			summary.Pages = cp.Pages
			summary.ListPages = cp.ListPages
			summary.Candidates = cp.Candidates
			summary.Details = cp.Details
			summary.DuplicateRecords = cp.DuplicateRecords
		}
		if err = finishState(s, r, summary); err != nil {
			return nil, err
		}
		rows = append(rows, *r)
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return rows, nil
}
func Results(ctx context.Context, id string, after int64, limit int) ([]table.V22RunDocument, error) {
	rows := []table.V22RunDocument{}
	err := db.Instance().Context(ctx).Where("run_id=? AND sequence>?", id, after).Asc("sequence").Limit(limit).Find(&rows)
	return rows, err
}
func Events(ctx context.Context, id string, after int64) ([]table.V22RunEvent, error) {
	rows := []table.V22RunEvent{}
	err := db.Instance().Context(ctx).Where("run_id=? AND sequence>?", id, after).Asc("sequence").Limit(50).Find(&rows)
	return rows, err
}
