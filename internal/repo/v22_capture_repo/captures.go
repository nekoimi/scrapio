package v22_capture_repo

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_governance_repo"
)

var ErrConflict = errors.New("capture idempotency input changed")
var ErrNotFound = errors.New("capture or collector not found")

func GetByKey(ownerID int64, key string) (*table.V22Capture, error) {
	row := new(table.V22Capture)
	has, err := db.Instance().Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return Get(ownerID, row.Id)
}
func Create(ownerID int64, collector *table.V22Collector, key string, input capture.Input, request capture.HTTPRequest) (*table.V22Capture, bool, error) {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, false, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "capture", key); err != nil {
		return nil, false, err
	}
	fingerprint := capture.Fingerprint(input, request, collector.EntryURL)
	previous := new(table.V22Capture)
	if has, err := s.Where("owner_id=? AND idempotency_key=?", ownerID, key).Get(previous); err != nil {
		return nil, false, err
	} else if has {
		var journal struct {
			Request capture.HTTPRequest `json:"http_request"`
			Target  string              `json:"target"`
		}
		if json.Unmarshal([]byte(previous.Request), &journal) != nil || previous.Fingerprint != capture.Fingerprint(input, journal.Request, journal.Target) {
			return nil, false, ErrConflict
		}
		return previous, false, nil
	}
	if _, err := s.QueryString("SELECT id FROM v22_collectors WHERE id=? AND owner_id=? FOR UPDATE", collector.Id, ownerID); err != nil {
		return nil, false, err
	}
	current := new(table.V22Collector)
	if has, err := s.Where("id=? AND owner_id=?", collector.Id, ownerID).Get(current); err != nil {
		return nil, false, err
	} else if !has || current.Status == "archived" {
		return nil, false, ErrNotFound
	}
	if current.Revision != input.ExpectedRevision || current.Definition != collector.Definition {
		return nil, false, &v22_collector_repo.RevisionConflict{Latest: current}
	}
	if err := v22_governance_repo.ReserveAssets(s, ownerID, capture.MaxBytes); err != nil {
		return nil, false, err
	}
	row := &table.V22Capture{SessionId: input.SessionID, PageStateId: input.PageStateID, Id: uuid.NewString(), OwnerId: ownerID, CollectorId: collector.Id, DraftRevision: collector.Revision, IdempotencyKey: key, Fingerprint: fingerprint, Request: capture.Journal(input, request, collector.EntryURL), Source: input.Source, Format: input.Format, Status: "running", CreatedAt: time.Now(), DeadlineAt: time.Now().Add(25 * time.Second)}
	if _, err := s.InsertOne(row); err != nil {
		return nil, false, err
	}
	if err := s.Commit(); err != nil {
		return nil, false, err
	}
	return row, true, nil
}
func Complete(row *table.V22Capture, result capture.Result) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, row.OwnerId, "governance.assets", "owner"); err != nil {
		return err
	}

	if err := v22_governance_repo.CompleteAssets(s, row.OwnerId, row.Id, int64(len(result.Content))); err != nil {
		if !errors.Is(err, v22_governance_repo.ErrCapacity) {
			return err
		}
		result.Content = ""
		result.Status = "failed"
		result.ErrorCode = "ASSET_CAPACITY"
		result.ErrorStage = "capacity"
		result.Bytes = 0
	}
	if len(result.Content) > capture.MaxBytes {
		result.Content = ""
		result.Status = "failed"
		result.ErrorCode = "INPUT_TOO_LARGE"
		result.ErrorStage = "capture"
		result.Bytes = 0
	}
	update := &table.V22Capture{BaseURL: result.BaseURL, Status: result.Status, FinalURL: result.FinalURL, ContentType: result.ContentType, StatusCode: result.StatusCode, Content: result.Content, ContentHash: result.Hash, ByteCount: result.Bytes, ErrorCode: result.ErrorCode, ErrorStage: result.ErrorStage, NetworkAccessed: result.NetworkAccessed}
	if _, err := s.Where("id=? AND owner_id=? AND status IN ('running','uncertain')", row.Id, row.OwnerId).Cols("base_url", "status", "final_url", "content_type", "status_code", "content", "content_hash", "byte_count", "error_code", "error_stage", "network_accessed").Update(update); err != nil {
		return err
	}
	return s.Commit()
}

func Get(ownerID int64, id string) (*table.V22Capture, error) {
	row := new(table.V22Capture)
	has, err := db.Instance().Where("id=? AND owner_id=?", id, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if row.Status == "running" && !row.DeadlineAt.After(time.Now()) {
		if _, err = db.Instance().Where("id=? AND owner_id=? AND status='running'", id, ownerID).Cols("status", "error_code", "error_stage").Update(&table.V22Capture{Status: "uncertain", ErrorCode: "OUTCOME_UNCERTAIN", ErrorStage: "recovery"}); err != nil {
			return nil, err
		}
		// Use a fresh bean: xorm can turn the previous nonzero status into a
		// query predicate and otherwise miss the just-updated terminal state.
		row = new(table.V22Capture)
		has, err = db.Instance().Where("id=? AND owner_id=?", id, ownerID).Get(row)
		if err == nil && !has {
			return nil, ErrNotFound
		}
	}
	return row, err
}
