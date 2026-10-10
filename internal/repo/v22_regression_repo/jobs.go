package v22_regression_repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/regression"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

var ErrNotFound = errors.New("regression resource not found")
var ErrConflict = errors.New("regression revision or key conflict")
var ErrInvalid = errors.New("invalid regression request")
var ErrCapacity = errors.New("regression capacity exceeded")
var ErrRestoreCapacity = errors.New("version restore capacity exceeded")

func session(ctx context.Context) *xorm.Session {
	return db.Instance().NewSession().Context(context.WithValue(ctx, xormlog.SessionShowSQLKey, false))
}
func hash(v any) string { raw, _ := json.Marshal(v); return publication.DefinitionHash(string(raw)) }

func Create(ctx context.Context, owner, id int64, key, kind string, i regression.Input) (*table.V22Regression, error) {
	if key == "" || len([]rune(key)) > 128 || i.ExpectedRevision < 1 || (kind != "regression" && kind != "comparison") || (kind == "comparison" && i.BaseVersionID == "") || (kind == "regression" && (i.BaseVersionID != "" || i.TargetVersionID != "")) {
		return nil, ErrInvalid
	}
	s := session(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	// Serialize capacity; the SELECT below freezes the batch at one statement snapshot.
	if err := idempotency.Lock(s, owner, "regression.capacity", "owner"); err != nil {
		return nil, err
	}
	prior := new(table.V22Regression)
	found, err := s.Where("owner_id=? AND idempotency_key=?", owner, key).Omit("snapshot").Get(prior)
	if err != nil {
		return nil, err
	}
	fp := hash([]any{id, kind, i})
	if found {
		if prior.Fingerprint != fp {
			return nil, ErrConflict
		}
		return prior, nil
	}
	n, err := s.Where("owner_id=? AND status IN ('queued','running')", owner).Count(new(table.V22Regression))
	if err != nil {
		return nil, err
	}
	if n >= 2 {
		return nil, ErrCapacity
	}
	n, err = s.Where("owner_id=?", owner).Count(new(table.V22Regression))
	if err != nil {
		return nil, err
	}
	if n >= 100 {
		return nil, ErrCapacity
	}
	// Load collector, versions and all samples/captures in ONE SELECT. This avoids
	// mixed revisions without locking collector before samples (lock order inversion).
	rows, err := s.QueryString(`SELECT to_jsonb(c)::text AS collector,
 CASE WHEN (SELECT COALESCE(SUM(COALESCE(octet_length(p.content),0)+octet_length(s.expected::text)),0)
 FROM v22_samples s LEFT JOIN v22_captures p ON p.id=s.capture_id AND p.owner_id=s.owner_id AND p.collector_id=s.collector_id
 WHERE s.owner_id=c.owner_id AND s.collector_id=c.id)>16777216 THEN 'null' ELSE
 COALESCE((SELECT jsonb_agg(jsonb_build_object('sample',to_jsonb(s)-'screenshot','capture',CASE WHEN p.id IS NULL THEN NULL ELSE jsonb_build_object('content',p.content,'content_hash',p.content_hash,'format',p.format,'final_url',p.final_url,'base_url',p.base_url,'status',p.status) END) ORDER BY s.created_at,s.id)
 FROM v22_samples s LEFT JOIN v22_captures p ON p.id=s.capture_id AND p.owner_id=s.owner_id AND p.collector_id=s.collector_id
 WHERE s.owner_id=c.owner_id AND s.collector_id=c.id),'[]'::jsonb)::text END AS samples,
 (SELECT to_jsonb(v)::text FROM v22_versions v WHERE v.id=? AND v.owner_id=c.owner_id AND v.collector_id=c.id) AS base,
 (SELECT to_jsonb(v)::text FROM v22_versions v WHERE v.id=? AND v.owner_id=c.owner_id AND v.collector_id=c.id) AS target
 FROM v22_collectors c WHERE c.owner_id=? AND c.id=?`, i.BaseVersionID, i.TargetVersionID, owner, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	if rows[0]["samples"] == "null" {
		return nil, ErrCapacity
	}
	// PostgreSQL column names contain underscores; decode an explicit wire shape.
	var c struct {
		Revision   int             `json:"revision"`
		Definition json.RawMessage `json:"definition"`
		Status     string          `json:"status"`
	}
	if json.Unmarshal([]byte(rows[0]["collector"]), &c) != nil {
		return nil, ErrConflict
	}
	if c.Revision != i.ExpectedRevision || c.Status == "archived" {
		return nil, ErrConflict
	}
	snap := regression.Snapshot{Target: c.Definition, Samples: []regression.Document{}}
	version := func(raw string) (json.RawMessage, error) {
		var v struct {
			Definition json.RawMessage `json:"definition"`
			Hash       string          `json:"definition_hash"`
		}
		if raw == "" {
			return nil, ErrNotFound
		}
		if json.Unmarshal([]byte(raw), &v) != nil || v.Hash != publication.DefinitionHash(string(v.Definition)) {
			return nil, ErrConflict
		}
		return v.Definition, nil
	}
	if i.BaseVersionID != "" {
		snap.Base, err = version(rows[0]["base"])
		if err != nil {
			return nil, err
		}
	}
	if i.TargetVersionID != "" {
		snap.Target, err = version(rows[0]["target"])
		if err != nil {
			return nil, err
		}
	}
	var batch []struct {
		Sample struct {
			ID           string          `json:"id"`
			Name         string          `json:"name"`
			Revision     int             `json:"revision"`
			Kind         string          `json:"kind"`
			StepID       string          `json:"step_id"`
			Stage        string          `json:"stage"`
			Expected     json.RawMessage `json:"expected"`
			ExpectedHash string          `json:"expected_hash"`
		} `json:"sample"`
		Capture *struct {
			Content     string `json:"content"`
			ContentHash string `json:"content_hash"`
			Format      string `json:"format"`
			URL         string `json:"final_url"`
			BaseURL     string `json:"base_url"`
			Status      string `json:"status"`
		} `json:"capture"`
	}
	if json.Unmarshal([]byte(rows[0]["samples"]), &batch) != nil {
		return nil, ErrConflict
	}
	if len(batch) > 100 {
		return nil, ErrCapacity
	}
	for _, b := range batch {
		p := b.Sample
		d := regression.Document{ID: p.ID, Name: p.Name, Revision: p.Revision, Kind: p.Kind, StepID: p.StepID, Stage: p.Stage, Expected: p.Expected, ExpectedHash: p.ExpectedHash}
		if b.Capture == nil {
			d.Error = "INPUT_MISSING"
		} else {
			v := b.Capture
			d.ContentHash = v.ContentHash
			d.Format = v.Format
			d.URL = v.URL
			d.BaseURL = v.BaseURL
			if v.Status != "succeeded" || v.Content == "" || len(v.Content) > capture.MaxBytes || capture.Hash([]byte(v.Content)) != v.ContentHash {
				d.Error = "INPUT_UNAVAILABLE"
			} else {
				d.Content = v.Content
			}
		}
		snap.Samples = append(snap.Samples, d)
	}
	raw, _ := json.Marshal(snap)
	if len(raw) > regression.MaxBatchBytes {
		return nil, ErrCapacity
	}
	now := time.Now()
	job := &table.V22Regression{Id: uuid.NewString(), OwnerId: owner, CollectorId: id, Kind: kind, CollectorRevision: c.Revision, BaseVersionId: i.BaseVersionID, TargetVersionId: i.TargetVersionID, BaseHash: publication.DefinitionHash(string(snap.Base)), TargetHash: publication.DefinitionHash(string(snap.Target)), Snapshot: string(raw), SnapshotHash: publication.DefinitionHash(string(raw)), Status: "queued", Total: len(batch), Report: "{}", DeadlineAt: now.Add(5 * time.Minute), IdempotencyKey: key, Fingerprint: fp, CreatedAt: now}
	if _, err = s.InsertOne(job); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}
func Get(ctx context.Context, owner int64, id, key string) (*table.V22Regression, error) {
	s := session(ctx)
	defer s.Close()
	r := new(table.V22Regression)
	s.Where("owner_id=?", owner)
	if id != "" {
		s.And("id=?", id)
	} else {
		s.And("idempotency_key=?", key)
	}
	has, err := s.Omit("snapshot", "fingerprint", "idempotency_key", "lease").Get(r)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return r, nil
}
func List(ctx context.Context, owner, id int64) ([]table.V22Regression, error) {
	s := session(ctx)
	defer s.Close()
	rows := []table.V22Regression{}
	err := s.Where("owner_id=? AND collector_id=?", owner, id).Omit("snapshot", "report", "fingerprint", "idempotency_key", "lease").Desc("created_at", "id").Limit(100).Find(&rows)
	return rows, err
}
func Cancel(ctx context.Context, owner int64, id string) (*table.V22Regression, error) {
	s := session(ctx)
	defer s.Close()
	_, err := s.Exec("UPDATE v22_regressions SET status='cancelled',finished_at=NOW(),lease='' WHERE owner_id=? AND id=? AND status IN ('queued','running')", owner, id)
	if err != nil {
		return nil, err
	}
	return Get(ctx, owner, id, "")
}
func Delete(ctx context.Context, owner int64, id string) error {
	s := session(ctx)
	defer s.Close()
	res, err := s.Exec("DELETE FROM v22_regressions WHERE owner_id=? AND id=? AND status NOT IN ('queued','running')", owner, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func Claim(ctx context.Context) (*table.V22Regression, error) {
	s := session(ctx)
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if _, err := s.Exec("UPDATE v22_regressions SET status='failed',error_code='INTERRUPTED',lease='',finished_at=NOW() WHERE status IN ('queued','running') AND deadline_at<NOW()"); err != nil {
		return nil, err
	}
	ids, err := s.QueryString("SELECT id FROM v22_regressions WHERE status='queued' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, s.Commit()
	}
	r := new(table.V22Regression)
	has, err := s.Where("id=?", ids[0]["id"]).Get(r)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	r.Lease = uuid.NewString()
	r.DeadlineAt = time.Now().Add(45 * time.Second)
	r.Status = "running"
	if _, err = s.ID(r.Id).Cols("lease", "deadline_at", "status").Update(r); err != nil {
		return nil, err
	}
	return r, s.Commit()
}
func Progress(ctx context.Context, j *table.V22Regression, n int) error {
	s := session(ctx)
	defer s.Close()
	res, err := s.Exec("UPDATE v22_regressions SET completed=? WHERE id=? AND lease=? AND status='running' AND deadline_at>NOW()", n, j.Id, j.Lease)
	if err != nil {
		return err
	}
	count, _ := res.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return nil
}
func Finish(ctx context.Context, j *table.V22Regression, report regression.Report, code string) error {
	status := "succeeded"
	raw := "{}"
	if code != "" {
		status = "failed"
	} else {
		data, err := json.Marshal(report)
		if err != nil || len(data) > regression.MaxReportBytes {
			status = "failed"
			code = "REPORT_BUDGET_EXCEEDED"
		} else {
			raw = string(data)
		}
	}
	s := session(ctx)
	defer s.Close()
	_, err := s.Exec("UPDATE v22_regressions SET status=?,report=?::jsonb,error_code=?,finished_at=NOW(),lease='' WHERE id=? AND lease=? AND status='running' AND deadline_at>NOW()", status, raw, code, j.Id, j.Lease)
	return err
}
