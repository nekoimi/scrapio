// Package v22_repair_repo binds an owned terminal failure to a draft and a
// detached offline snapshot in one transaction. It never updates the source run.
package v22_repair_repo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repair"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

var (
	ErrNotFound = errors.New("repair source or target not found")
	ErrConflict = errors.New("repair request or draft changed")
	ErrInvalid  = errors.New("repair requires a failed or partial run")
	ErrCapacity = errors.New("repair context capacity reached")
)

func fingerprint(id int64, i repair.Input) string {
	raw, _ := json.Marshal([]any{id, i})
	return capture.Hash(raw)
}
func quiet(ctx context.Context) context.Context {
	return context.WithValue(ctx, xormlog.SessionShowSQLKey, false)
}

// No candidate from another attempt, step or role is silently substituted.
func source(s *xorm.Session, owner int64, runID, documentID string) (*repair.Context, *table.V22Run, *table.V22Collector, *table.V22RunDocument, *trial.Document, error) {
	r := new(table.V22Run)
	has, err := s.Where("id=? AND owner_id=?", runID, owner).Get(r)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if !has {
		return nil, nil, nil, nil, nil, ErrNotFound
	}
	if !repair.Eligible(r.Status) {
		return nil, nil, nil, nil, nil, ErrInvalid
	}
	if r.DefinitionHash != publication.DefinitionHash(r.Definition) {
		return nil, nil, nil, nil, nil, ErrConflict
	}
	c := new(table.V22Collector)
	has, err = s.Where("id=? AND owner_id=?", r.CollectorId, owner).Get(c)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if !has {
		return nil, nil, nil, nil, nil, ErrNotFound
	}
	var summary trial.Summary
	if json.Unmarshal([]byte(r.Summary), &summary) != nil {
		return nil, nil, nil, nil, nil, ErrConflict
	}
	h := &repair.Context{SourceCollectorID: strconv.FormatInt(c.Id, 10), RunID: r.Id, VersionID: r.VersionId, VersionNumber: r.VersionNumber, CurrentPublishedVersionID: c.PublishedVersionId, TargetRevision: c.Revision, PendingDraft: c.PublishedVersionId == nil, Archived: c.Status == "archived", StepID: summary.FailedStep, Stage: summary.FailedStage, Reason: summary.Reason, FieldKeys: []string{}}
	h.FailedStepID = summary.FailedStep
	h.FailedStage = summary.FailedStage
	if c.PublishedVersionId != nil {
		v := new(table.V22Version)
		found, e := s.Where("id=? AND owner_id=? AND collector_id=?", *c.PublishedVersionId, owner, c.Id).Get(v)
		if e != nil {
			return nil, nil, nil, nil, nil, e
		}
		h.PendingDraft = !found || c.Revision > v.CollectorRevision
		if found {
			h.CurrentPublishedVersionNumber = v.Number
		}
	}
	// Output validation can report no specific field/step: use the frozen output
	// role, then prefer a saved invalid/empty document in that role.
	if h.StepID == "" || h.Stage == "output" {
		var root struct {
			Output struct {
				StepID string `json:"step_id"`
				Stage  string `json:"stage"`
			} `json:"output"`
		}
		_ = json.Unmarshal([]byte(r.Definition), &root)
		if h.Stage == "output" || h.Reason == "OUTPUT_VALIDATION_FAILED" || h.Reason == "NO_VALID_CANDIDATES" || h.Reason == "VALIDATION_FAILED" {
			h.StepID = root.Output.StepID
			h.Stage = root.Output.Stage
		}
	}
	d := new(table.V22RunDocument)
	if documentID != "" {
		has, err = s.Where("id=? AND run_id=?", documentID, r.Id).Get(d)
		if err == nil && !has {
			return nil, nil, nil, nil, nil, ErrNotFound
		}
	} else if h.StepID != "" && (h.Stage == "list" || h.Stage == "detail") {
		rows, e := s.QueryString(`SELECT id FROM v22_run_documents WHERE run_id=? AND attempt=? AND result->>'step_id'=? AND result->>'stage'=? ORDER BY (COALESCE((result->'extraction'->>'invalid_count')::int,0)>0 OR COALESCE((result->'extraction'->>'match_count')::int,0)=0) DESC,sequence DESC LIMIT 1`, r.Id, r.Attempt, h.StepID, h.Stage)
		if e != nil {
			return nil, nil, nil, nil, nil, e
		}
		if len(rows) > 0 {
			has, err = s.Where("id=? AND run_id=?", rows[0]["id"], r.Id).Get(d)
		}
	}
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var metadata *trial.Document
	if has && d.Id != "" {
		h.DocumentID = d.Id
		metadata = new(trial.Document)
		if json.Unmarshal([]byte(d.Result), metadata) != nil {
			metadata = nil
		} else {
			h.StepID = metadata.StepID
			h.Stage = metadata.Stage
			h.Format = metadata.Format
			h.FieldKeys = repair.Fields(*metadata)
		}
	} else {
		d = nil
	}
	h.Evidence = repair.Evidence(d, metadata)
	h.StepCompatible = repair.Compatible(c.Definition, h.StepID, h.Stage, h.Format)
	return h, r, c, d, metadata, nil
}

func Prepare(ctx context.Context, owner int64, runID, documentID string) (*repair.Context, error) {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	h, _, _, _, _, err := source(s, owner, runID, documentID)
	return h, err
}

func decode(s *xorm.Session, owner int64, row *table.V22RepairDraft) (*repair.Context, error) {
	var h repair.Context
	if json.Unmarshal([]byte(row.Context), &h) != nil {
		return nil, ErrConflict
	}
	// A detached capture may have been removed; preserve diagnostic metadata and
	// report missing instead of fetching a new page or using another capture.
	if h.CaptureID != "" {
		c := new(table.V22Capture)
		has, err := s.Where("id=? AND owner_id=? AND collector_id=?", h.CaptureID, owner, row.TargetCollectorId).Get(c)
		if err != nil {
			return nil, err
		}
		if !has || row.CaptureId == nil || c.Content == "" {
			h.CaptureID = ""
			h.Evidence = "missing"
		} else if len(c.Content) > capture.MaxBytes {
			h.CaptureID = ""
			h.Evidence = "too_large"
		} else if c.Status != "succeeded" || c.ContentHash != capture.Hash([]byte(c.Content)) {
			h.Evidence = "corrupt"
			h.CaptureID = ""
		}
	}
	return &h, nil
}

func Get(ctx context.Context, owner int64, id, key string) (*repair.Context, error) {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	row := new(table.V22RepairDraft)
	s.Where("owner_id=?", owner)
	if id != "" {
		s.And("id=?", id)
	} else {
		s.And("idempotency_key=?", key)
	}
	has, err := s.Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return decode(s, owner, row)
}

func Create(ctx context.Context, owner, id int64, key string, i repair.Input) (*repair.Context, error) {
	if i.Validate() != nil || key == "" || len([]rune(key)) > 128 {
		return nil, ErrInvalid
	}
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	// Owner lock also bounds capacity when different keys are submitted at once.
	if err := idempotency.Lock(s, owner, "repair.capacity", "owner"); err != nil {
		return nil, err
	}
	prior := new(table.V22RepairDraft)
	has, err := s.Where("owner_id=? AND idempotency_key=?", owner, key).Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint(id, i) {
			return nil, ErrConflict
		}
		return decode(s, owner, prior)
	}
	if _, err = s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", id, owner); err != nil {
		return nil, err
	}
	h, r, c, d, meta, err := source(s, owner, i.RunID, i.DocumentID)
	if err != nil {
		return nil, err
	}
	if c.Id != id {
		return nil, ErrNotFound
	}
	if c.Revision != i.ExpectedRevision {
		return nil, ErrConflict
	}
	if i.Mode == "continue" && c.Status == "archived" {
		return nil, ErrConflict
	}
	n, err := s.Where("owner_id=?", owner).Count(new(table.V22RepairDraft))
	if err != nil {
		return nil, err
	}
	if n >= 100 {
		return nil, ErrCapacity
	}
	now := time.Now()
	target := c
	if i.Mode == "fork" {
		definition, e := repair.ForkDefinition(r.Definition)
		if e != nil {
			return nil, e
		}
		var root struct {
			EntryURL string `json:"entry_url"`
		}
		if json.Unmarshal([]byte(definition), &root) != nil || root.EntryURL == "" {
			return nil, ErrConflict
		}
		target = &table.V22Collector{OwnerId: owner, Name: repair.ForkName(c.Name), EntryURL: root.EntryURL, EntryType: r.EntryType, Status: "draft", Definition: definition, Revision: 1, ValidationSummary: `{"valid":false,"errors":[]}`, CreatedBy: owner, UpdatedBy: owner, CreatedAt: now, UpdatedAt: now}
		if _, err = s.InsertOne(target); err != nil {
			return nil, err
		}
	}
	h.ID = uuid.NewString()
	h.TargetCollectorID = strconv.FormatInt(target.Id, 10)
	h.TargetRevision = target.Revision
	h.Mode = i.Mode
	h.StepCompatible = repair.Compatible(target.Definition, h.StepID, h.Stage, h.Format)
	row := &table.V22RepairDraft{Id: h.ID, OwnerId: owner, SourceCollectorId: id, TargetCollectorId: target.Id, RunId: r.Id, IdempotencyKey: key, Fingerprint: fingerprint(id, i), CreatedAt: now}
	if h.Evidence == "available" {
		captureID := uuid.NewString()
		journal, _ := json.Marshal(map[string]string{"repair_id": h.ID, "run_id": r.Id, "document_id": d.Id})
		snap := &table.V22Capture{Id: captureID, OwnerId: owner, CollectorId: target.Id, DraftRevision: target.Revision, IdempotencyKey: "repair:" + h.ID, Fingerprint: capture.Hash(journal), Request: string(journal), Source: "offline", Format: meta.Format, Status: "succeeded", FinalURL: meta.URL, BaseURL: meta.BaseURL, ContentType: "text/html", Content: d.Content, ContentHash: meta.ContentHash, ByteCount: len(d.Content), CreatedAt: now, DeadlineAt: now}
		if meta.Format == "json" {
			snap.ContentType = "application/json"
		}
		if _, err = s.InsertOne(snap); err != nil {
			return nil, err
		}
		h.CaptureID = captureID
		row.CaptureId = &captureID
	}
	raw, _ := json.Marshal(h)
	row.Context = string(raw)
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return h, nil
}
