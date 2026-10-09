package v22_session_repo

import (
	"errors"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
)

var ErrNotFound = errors.New("browser session not found")
var ErrRevisionConflict = errors.New("collector revision changed")
var ErrIdempotencyConflict = errors.New("idempotency key was already used for another session request")
var ErrUnsupportedEntry = errors.New("browser sessions require a web collector")
var ErrSessionGone = errors.New("browser session ended or changed")

func Create(ownerID, collectorID int64, expectedRevision int, sessionID, idempotencyKey string, width, height int, ttl time.Duration) (*table.V22BrowserSession, error) {
	if ownerID <= 0 || collectorID <= 0 || expectedRevision <= 0 || sessionID == "" || idempotencyKey == "" || len([]rune(idempotencyKey)) > 128 || db.Instance() == nil {
		return nil, errors.New("invalid browser session request")
	}
	if ttl < 30*time.Second || ttl > 10*time.Minute || width < 320 || width > 3840 || height < 240 || height > 2160 {
		return nil, errors.New("browser session limits exceeded")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, ownerID, "session.create", idempotencyKey); err != nil {
		return nil, err
	}
	var key table.V22BrowserSessionKey
	if has, err := s.Where("owner_id = ? AND key = ?", ownerID, idempotencyKey).Get(&key); err != nil {
		return nil, err
	} else if has {
		if key.CollectorId != collectorID || key.DraftRevision != expectedRevision {
			return nil, ErrIdempotencyConflict
		}
		row := new(table.V22BrowserSession)
		if has, err := s.Where("id = ? AND owner_id = ?", key.SessionId, ownerID).Get(row); err != nil || !has {
			return nil, ErrNotFound
		}
		if row.ViewportWidth != width || row.ViewportHeight != height {
			return nil, ErrIdempotencyConflict
		}
		return row, nil
	}
	rows, err := s.QueryString("SELECT id FROM v22_collectors WHERE id = ? AND owner_id = ? AND status <> ? FOR UPDATE", collectorID, ownerID, "archived")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	collector := new(table.V22Collector)
	if has, err := s.ID(collectorID).Get(collector); err != nil || !has {
		return nil, ErrNotFound
	}
	if collector.EntryType != "web" {
		return nil, ErrUnsupportedEntry
	}
	if collector.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	now := time.Now()
	row := &table.V22BrowserSession{
		Id: sessionID, OwnerId: ownerID, CollectorId: collectorID, DraftRevision: expectedRevision,
		TargetURL: collector.EntryURL, Status: "creating", ExpiresAt: now.Add(ttl), ViewportWidth: width, ViewportHeight: height,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err := s.InsertOne(&table.V22BrowserSessionKey{OwnerId: ownerID, Key: idempotencyKey, SessionId: sessionID, CollectorId: collectorID, DraftRevision: expectedRevision, CreatedAt: now}); err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func Get(ownerID int64, sessionID string) (*table.V22BrowserSession, error) {
	if ownerID <= 0 || sessionID == "" || db.Instance() == nil {
		return nil, ErrNotFound
	}
	row := new(table.V22BrowserSession)
	has, err := db.Instance().Where("id = ? AND owner_id = ?", sessionID, ownerID).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	return row, nil
}

func UpdateFromBrowser(ownerID int64, sessionID, status, pageStateID, currentURL string, expiresAt time.Time) error {
	if ownerID <= 0 || sessionID == "" || !validStatus(status) {
		return errors.New("invalid browser session state")
	}
	affected, err := db.Instance().Where("id = ? AND owner_id = ? AND status IN ('creating', 'ready', 'disconnected')", sessionID, ownerID).Cols("status", "page_state_id", "current_url", "expires_at", "updated_at").Update(&table.V22BrowserSession{
		Status: status, PageStateId: pageStateID, CurrentURL: currentURL, ExpiresAt: expiresAt, UpdatedAt: time.Now(),
	})
	if err == nil && affected == 0 {
		return ErrSessionGone
	}
	return err
}

func SetStatus(ownerID int64, sessionID, status string) error {
	if ownerID <= 0 || sessionID == "" || !validStatus(status) {
		return errors.New("invalid browser session state")
	}
	values := &table.V22BrowserSession{Status: status, UpdatedAt: time.Now()}
	columns := []string{"status", "updated_at"}
	if status == "closed" || status == "expired" || status == "failed" {
		now := time.Now()
		values.ClosedAt = &now
		columns = append(columns, "closed_at")
	}
	condition := "id = ? AND owner_id = ?"
	if status == "disconnected" || status == "expired" || status == "failed" {
		condition += " AND status IN ('creating', 'ready', 'disconnected')"
	}
	affected, err := db.Instance().Where(condition, sessionID, ownerID).Cols(columns...).Update(values)
	if err == nil && affected == 0 {
		return ErrSessionGone
	}
	return err
}

func validStatus(status string) bool {
	switch status {
	case "creating", "ready", "disconnected", "expired", "closed", "failed":
		return true
	default:
		return false
	}
}
