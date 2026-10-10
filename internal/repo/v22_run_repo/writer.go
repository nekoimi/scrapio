package v22_run_repo

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
)

func finishState(s *xorm.Session, r *table.V22Run, summary runmodel.Summary) error {
	if !runmodel.Terminal(summary.Status) {
		return invalid("terminal result required")
	}
	r.Status = summary.Status
	raw, _ := json.Marshal(summary)
	if len(raw) > 8*capture.MaxBytes {
		return invalid("formal result exceeds 8 MiB")
	}
	r.Summary = string(raw)
	now := time.Now()
	r.FinishedAt = &now
	if _, err := s.Exec("UPDATE v22_runs SET lease_until=NULL WHERE id=?", r.Id); err != nil {
		return err
	}
	if _, err := s.Exec("UPDATE v22_run_attempts SET status=?,summary=?::jsonb,finished_at=? WHERE run_id=? AND attempt=? AND lease_token=?", r.Status, r.Summary, now, r.Id, r.Attempt, r.LeaseToken); err != nil {
		return err
	}
	return event(s, r, trial.Event{StepID: summary.FailedStep, Stage: summary.FailedStage, Status: summary.Status, Code: summary.Reason})
}

// Finish commits records, observations, revisions and the terminal result in
// one transaction. A lost commit response is recovered by reading the run; a
// terminal row returns immediately and never rewrites its observations.
func Finish(ctx context.Context, id, token string, execution trial.Summary) error {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	r, err := lock(s, id)
	if err != nil {
		return err
	}
	if r.LeaseToken != token || token == "" {
		return ErrLease
	}
	if runmodel.Terminal(r.Status) {
		return nil
	}
	rows, err := s.QueryString("SELECT id FROM v22_runs WHERE id=? AND status='running' AND lease_token=? AND lease_until>clock_timestamp()", id, token)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrLease
	}
	summary := runmodel.Empty(execution)
	if r.CancelRequested {
		summary.Status = "cancelled"
		summary.Reason = "CANCEL_REQUESTED"
		summary.Output = nil
	}
	if summary.Status == "succeeded" || summary.Status == "limited" {
		if err = write(s, r, &summary); err != nil {
			return err
		}
	}
	// Reject a transaction whose lease expired while waiting for the shared table.
	rows, err = s.QueryString("SELECT id FROM v22_runs WHERE id=? AND lease_token=? AND lease_until>clock_timestamp()", id, token)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrLease
	}
	if err = finishState(s, r, summary); err != nil {
		return err
	}
	return s.Commit()
}
func write(s *xorm.Session, r *table.V22Run, summary *runmodel.Summary) error {
	if summary.Output == nil || summary.FailedStep != "" || summary.FailedStage != "" {
		summary.Status = "failed"
		summary.Reason = "INCOMPLETE_EXECUTION_NOT_COMMITTED"
		return nil
	}
	plan, err := trial.Compile([]byte(r.Definition), r.EntryType)
	if err != nil {
		return ErrConflict
	}
	schema, err := output.DecodeSchema([]byte(r.OutputSchema))
	if err != nil {
		return err
	}
	if publication.DefinitionHash(r.Definition) != r.DefinitionHash || output.SchemaHash(schema) != plan.Output.SchemaHash {
		return ErrConflict
	}
	tableID, _ := strconv.ParseInt(plan.Output.TableID, 10, 64)
	// Serializing a logical table also protects absent keys from concurrent insert
	// races. B06 has one formal worker; schema writers must honor this lock too.
	if _, err = s.QueryString("SELECT id FROM v22_data_tables WHERE id=? AND owner_id=? FOR UPDATE", tableID, r.OwnerId); err != nil {
		return err
	}
	target := new(table.V22DataTable)
	has, err := s.Where("id=? AND owner_id=?", tableID, r.OwnerId).Get(target)
	if err != nil {
		return err
	}
	if !has {
		return ErrNotFound
	}
	if target.SchemaHash != plan.Output.SchemaHash || target.SchemaVersion != plan.Output.SchemaVersion {
		return ErrConflict
	}
	documentRows := []table.V22RunDocument{}
	if err = s.Where("run_id=? AND attempt=?", r.Id, r.Attempt).Asc("sequence").Find(&documentRows); err != nil {
		return err
	}
	docs := []trial.Document{}
	invalid := false
	for _, row := range documentRows {
		var doc trial.Document
		if runmodel.Decode(row.Result, &doc) != nil || doc.ID != row.Id || doc.ContentHash != capture.Hash([]byte(row.Content)) {
			return ErrConflict
		}
		docs = append(docs, doc)
		invalid = invalid || doc.Result.InvalidCount > 0 || len(doc.Result.Records) == 0
	}
	selected, origins := runmodel.Select(docs, plan.Output)
	existing := map[string]output.Existing{}
	for _, candidate := range selected.Records {
		p, issues := output.Prepare(schema, plan.Output.Mapping, candidate)
		if len(issues) > 0 {
			continue
		}
		if _, seen := existing[p.Key]; seen {
			continue
		}
		found, err := s.QueryString("SELECT id,revision,record_values::text AS record_values FROM v22_table_records WHERE table_id=? AND canonical_key=? FOR UPDATE", tableID, p.Key)
		if err != nil {
			return err
		}
		if len(found) > 0 {
			values, err := output.DecodeValues(found[0]["record_values"])
			if err != nil {
				return err
			}
			revision, _ := strconv.Atoi(found[0]["revision"])
			existing[p.Key] = output.Existing{RecordID: found[0]["id"], Revision: revision, Values: values}
		}
	}
	var compat output.Compatibility
	for _, step := range plan.Steps {
		if step.ID == plan.Output.StepID {
			compat = output.CheckCompatibility(schema, plan.Output.Mapping, output.SourceFields(step.Plan, plan.Output.Stage))
		}
	}
	preview := output.PreviewBatch(schema, plan.Output.Mapping, plan.Output.UpdatePolicy, plan.Output.EmptyPolicy, selected, compat, existing)
	summary.Output = &preview
	if invalid || !runmodel.Writable(summary.Summary, preview) {
		summary.Status = "partial"
		summary.Reason = "OUTPUT_VALIDATION_FAILED"
		if len(selected.Records) == 0 {
			summary.Status = "failed"
			summary.Reason = "NO_VALID_CANDIDATES"
		}
		return nil
	}
	// Preview's final values differ from incoming observations when merging or
	// keep_existing applies. Save the original mapped candidate for provenance.
	now := time.Now()
	byKey := map[string]output.Existing{}
	for _, decision := range preview.Rows {
		origin := origins[decision.Index]
		incoming, issues := output.Prepare(schema, plan.Output.Mapping, selected.Records[decision.Index])
		if len(issues) > 0 {
			return ErrConflict
		}
		writeKey := capture.Hash([]byte(r.Id + "/" + strconv.Itoa(r.Attempt) + "/" + origin.DocumentID + "/" + strconv.Itoa(origin.RecordIndex)))
		observationID := uuid.NewString()
		previous, duplicate := byKey[decision.Key]
		if !duplicate {
			previous = existing[decision.Key]
		}
		recordID, revision := previous.RecordID, previous.Revision
		if decision.Decision == "created" {
			recordID = uuid.NewString()
			revision = 1
			if _, err = s.InsertOne(&table.V22TableRecord{Id: recordID, TableId: tableID, SchemaVersion: plan.Output.SchemaVersion, CanonicalKey: decision.Key, KeyValues: decision.KeyJSON, RecordValues: decision.ValuesJSON, ValuesHash: decision.Hash, Revision: revision, FirstObservedAt: now, LastObservedAt: now}); err != nil {
				return err
			}
		} else {
			if recordID == "" {
				return ErrConflict
			}
			if decision.Decision == "updated" {
				revision++
			}
			if _, err = s.Exec("UPDATE v22_table_records SET schema_version=?,record_values=?::jsonb,values_hash=?,revision=?,last_observed_at=? WHERE id=?", plan.Output.SchemaVersion, decision.ValuesJSON, decision.Hash, revision, now, recordID); err != nil {
				return err
			}
		}
		if _, err = s.InsertOne(&table.V22TableObservation{Id: observationID, RecordId: recordID, TableId: tableID, SchemaVersion: plan.Output.SchemaVersion, CollectorId: &r.CollectorId, RunId: r.Id, VersionId: r.VersionId, DocumentId: origin.DocumentID, SourceURL: origin.URL, PageRole: origin.Stage, ObservedValues: incoming.ValuesJSON, ObservedValuesHash: incoming.Hash, WriteKey: writeKey, Outcome: decision.Decision, ObservedAt: now}); err != nil {
			return err
		}
		if decision.Decision == "created" || decision.Decision == "updated" {
			changed, _ := json.Marshal(decision.ChangedFields)
			if _, err = s.InsertOne(&table.V22TableRecordRevision{RecordId: recordID, Revision: revision, SchemaVersion: plan.Output.SchemaVersion, RecordValues: decision.ValuesJSON, ValuesHash: decision.Hash, ChangedFields: string(changed), ObservationId: observationID, CreatedAt: now}); err != nil {
				return err
			}
		}
		byKey[decision.Key] = output.Existing{RecordID: recordID, Revision: revision, Values: decision.Values}
		summary.Counts[decision.Decision]++
		summary.Writes = append(summary.Writes, runmodel.Write{Origin: origin, RecordID: recordID, ObservationID: observationID, Decision: decision.Decision, Revision: revision, ChangedFields: decision.ChangedFields, ValuesJSON: decision.ValuesJSON})
	}
	preview.DryRun = false
	preview.Warnings = []string{}
	if selected.Truncated {
		preview.Warnings = append(preview.Warnings, "COVERAGE_INCOMPLETE")
	}
	preview.Ready = true
	summary.Committed = true
	return nil
}
