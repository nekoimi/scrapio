package v22_query_repo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/dataquery"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
)

func fmtID(id int64) string { return strconv.FormatInt(id, 10) }

type ExportInput struct {
	Format     string          `json:"format"`
	SnapshotID string          `json:"snapshot_id"`
	Query      dataquery.Query `json:"query"`
	Confirmed  bool            `json:"confirmed"`
}

func CreateExport(ctx context.Context, owner, id int64, key string, input ExportInput) (*table.V22Export, error) {
	if !input.Confirmed || (input.Format != "csv" && input.Format != "json") || key == "" || len([]rune(key)) > 128 {
		return nil, invalid(errors.New("confirmed export, csv/json format and request key required"))
	}
	if _, err := uuid.Parse(input.SnapshotID); err != nil {
		return nil, invalid(errors.New("query snapshot required; first confirm the data page"))
	}
	requestJSON, _ := json.Marshal([]any{id, input})
	fingerprint := capture.Hash(requestJSON)
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	if err := idempotency.Lock(s, owner, "export.create", key); err != nil {
		return nil, err
	}
	prior := new(table.V22Export)
	has, err := s.Where("owner_id=? AND idempotency_key=?", owner, key).Omit("file").Get(prior)
	if err != nil {
		return nil, err
	}
	if has {
		if prior.Fingerprint != fingerprint {
			return nil, ErrConflict
		}
		return prior, nil
	}
	if _, err = loadTable(s, owner, id); err != nil {
		return nil, err
	}
	if err = idempotency.Lock(s, owner, "exports.capacity", "owner"); err != nil {
		return nil, err
	}
	active, err := s.Where("owner_id=? AND status IN ('queued','running')", owner).Count(new(table.V22Export))
	if err != nil {
		return nil, err
	}
	recent, err := s.Where("owner_id=?", owner).Count(new(table.V22Export))
	if err != nil {
		return nil, err
	}
	files, err := s.Where("owner_id=? AND expires_at>clock_timestamp() AND status IN ('queued','running','succeeded')", owner).Count(new(table.V22Export))
	if err != nil {
		return nil, err
	}
	if active >= 3 || recent >= 200 || files >= 20 {
		return nil, ErrCapacity
	}
	if _, err = s.QueryString("SELECT id FROM v22_data_snapshots WHERE id=? AND owner_id=? AND table_id=? FOR SHARE", input.SnapshotID, owner, id); err != nil {
		return nil, err
	}
	snap, err := get(s, owner, id, input.SnapshotID)
	if err != nil {
		return nil, err
	}
	schema, err := output.DecodeSchema([]byte(snap.Schema))
	if err != nil {
		return nil, err
	}
	q, err := dataquery.Normalize(input.Query, schema)
	if err != nil {
		return nil, invalid(err)
	}
	queryJSON, _ := json.Marshal(q)
	if capture.Hash(queryJSON) != snap.QueryHash {
		return nil, ErrConflict
	}
	now := time.Now().UTC()
	row := &table.V22Export{Id: uuid.NewString(), OwnerId: owner, TableId: id, SnapshotId: snap.Id, Format: input.Format, Status: "queued", RowCount: snap.Count, Query: snap.Query, Schema: snap.Schema, CapturedAt: snap.CapturedAt, IdempotencyKey: key, Fingerprint: fingerprint, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if _, err = s.InsertOne(row); err != nil {
		return nil, err
	}
	if _, err = s.Exec("UPDATE v22_data_snapshots SET expires_at=GREATEST(expires_at,?) WHERE id=?", row.ExpiresAt, snap.Id); err != nil {
		return nil, err
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func Export(ctx context.Context, owner int64, id, key string) (*table.V22Export, error) {
	row := new(table.V22Export)
	s := db.Instance().Context(quiet(ctx)).Where("owner_id=?", owner)
	if key != "" {
		s.And("idempotency_key=?", key)
	} else {
		s.And("id=?", id)
	}
	has, err := s.Omit("file").Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, v22_data_repo.ErrNotFound
	}
	if !row.ExpiresAt.After(time.Now()) && row.Status == "succeeded" {
		row.Status = "expired"
	}
	return row, nil
}
func Exports(ctx context.Context, owner, id int64, cursor string, limit int) ([]table.V22Export, bool, error) {
	if limit < 1 || limit > 50 {
		return nil, false, v22_data_repo.ErrInvalid
	}
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if _, err := loadTable(s, owner, id); err != nil {
		return nil, false, err
	}
	query := s.Where("owner_id=? AND table_id=?", owner, id)
	if cursor != "" {
		anchor, err := Export(ctx, owner, cursor, "")
		if err != nil {
			return nil, false, err
		}
		if anchor.TableId != id {
			return nil, false, v22_data_repo.ErrNotFound
		}
		query.And("(created_at,id)<(?,?)", anchor.CreatedAt, anchor.Id)
	}
	rows := []table.V22Export{}
	err := query.Omit("file").Desc("created_at", "id").Limit(limit + 1).Find(&rows)
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	for i := range rows {
		if !rows[i].ExpiresAt.After(time.Now()) && rows[i].Status == "succeeded" {
			rows[i].Status = "expired"
		}
	}
	return rows, more, err
}
func CancelExport(ctx context.Context, owner int64, id string) (*table.V22Export, error) {
	_, err := db.Instance().Context(quiet(ctx)).Exec("UPDATE v22_exports SET status='cancelled',lease_token='',lease_until=NULL,finished_at=clock_timestamp(),snapshot_id=NULL,file=NULL WHERE id=? AND owner_id=? AND status IN ('queued','running')", id, owner)
	if err != nil {
		return nil, err
	}
	return Export(ctx, owner, id, "")
}
func Download(ctx context.Context, owner int64, id string) (*table.V22Export, error) {
	row := new(table.V22Export)
	has, err := db.Instance().Context(quiet(ctx)).Where("id=? AND owner_id=?", id, owner).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, v22_data_repo.ErrNotFound
	}
	if !row.ExpiresAt.After(time.Now()) || row.Status == "expired" {
		return nil, ErrExpired
	}
	if row.Status != "succeeded" {
		return nil, ErrConflict
	}
	if len(row.File) != row.FileBytes || capture.Hash(row.File) != row.FileHash {
		return nil, errors.New("export file integrity mismatch")
	}
	return row, nil
}
func Cleanup(ctx context.Context) error {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if _, err := s.Exec("UPDATE v22_exports SET status='expired',snapshot_id=NULL,file=NULL,lease_token='',lease_until=NULL WHERE expires_at<=clock_timestamp() AND status<>'expired'"); err != nil {
		return err
	}
	if _, err := s.Exec("UPDATE v22_exports SET status='failed',error_code='EXECUTOR_INTERRUPTED',snapshot_id=NULL,lease_token='',lease_until=NULL,finished_at=clock_timestamp() WHERE status='running' AND lease_until<=clock_timestamp()"); err != nil {
		return err
	}
	if _, err := s.Exec("DELETE FROM v22_exports WHERE created_at<clock_timestamp()-INTERVAL '7 days' AND status NOT IN ('queued','running')"); err != nil {
		return err
	}
	if _, err := s.Exec("DELETE FROM v22_data_snapshots p WHERE expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM v22_exports e WHERE e.snapshot_id=p.id)"); err != nil {
		return err
	}
	return s.Commit()
}
