package v22_query_repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
)

func Claim(ctx context.Context) (*table.V22Export, error) {
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM v22_exports WHERE status='queued' AND expires_at>clock_timestamp() ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1")
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	id := rows[0]["id"]
	token := uuid.NewString()
	if _, err = s.Exec("UPDATE v22_exports SET status='running',lease_token=?,lease_until=clock_timestamp()+INTERVAL '30 seconds' WHERE id=?", token, id); err != nil {
		return nil, err
	}
	row := new(table.V22Export)
	has, err := s.ID(id).Omit("file").Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrConflict
	}
	if err = s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
func ExportRows(ctx context.Context, job *table.V22Export) ([]map[string]string, error) {
	row := new(table.V22DataSnapshot)
	has, err := db.Instance().Context(quiet(ctx)).Where("id=? AND owner_id=? AND table_id=?", job.SnapshotId, job.OwnerId, job.TableId).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrExpired
	}
	rows := []map[string]string{}
	err = json.Unmarshal([]byte(row.Rows), &rows)
	return rows, err
}
func Progress(ctx context.Context, job *table.V22Export, count int) error {
	result, err := db.Instance().Context(quiet(ctx)).Exec("UPDATE v22_exports SET progress=? WHERE id=? AND lease_token=? AND status='running' AND lease_until>clock_timestamp()", count, job.Id, job.LeaseToken)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func Finish(ctx context.Context, job *table.V22Export, file []byte, code string) error {
	status := "succeeded"
	if code != "" {
		status = "failed"
		file = nil
	}
	_, err := db.Instance().Context(quiet(ctx)).Exec("UPDATE v22_exports SET status=?,file=?,file_hash=?,file_bytes=?,error_code=?,finished_at=?,snapshot_id=NULL,lease_token='',lease_until=NULL,progress=CASE WHEN ?='succeeded' THEN row_count ELSE progress END WHERE id=? AND lease_token=? AND status='running' AND lease_until>clock_timestamp()", status, file, capture.Hash(file), len(file), code, time.Now().UTC(), status, job.Id, job.LeaseToken)
	return err
}
