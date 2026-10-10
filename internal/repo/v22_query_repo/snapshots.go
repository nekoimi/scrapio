package v22_query_repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/dataquery"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/idempotency"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
	"xorm.io/xorm"
	xormlog "xorm.io/xorm/log"
)

var ErrExpired = errors.New("snapshot or export expired")
var ErrConflict = errors.New("query, view revision or request key conflict")
var ErrCapacity = errors.New("query or export capacity exceeded")

func quiet(ctx context.Context) context.Context {
	return context.WithValue(ctx, xormlog.SessionShowSQLKey, false)
}

func invalid(err error) error { return errors.Join(v22_data_repo.ErrInvalid, err) }
func loadTable(s *xorm.Session, owner, id int64) (*table.V22DataTable, error) {
	row := new(table.V22DataTable)
	has, err := s.Where("id=? AND owner_id=?", id, owner).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, v22_data_repo.ErrNotFound
	}
	return row, nil
}
func Normalize(s *xorm.Session, owner, id int64, q dataquery.Query) (dataquery.Query, output.Schema, error) {
	row, err := loadTable(s, owner, id)
	if err != nil {
		return q, output.Schema{}, err
	}
	schema, err := output.DecodeSchema([]byte(row.Schema))
	if err != nil {
		return q, schema, err
	}
	q, err = dataquery.Normalize(q, schema)
	if err != nil {
		return q, schema, invalid(err)
	}
	return q, schema, nil
}
func create(s *xorm.Session, owner, id int64, q dataquery.Query) (*table.V22DataSnapshot, error) {
	// One repeatable-read transaction captures committed values, order and schema together.
	if err := idempotency.Lock(s, owner, "query.capacity", "owner"); err != nil {
		return nil, err
	}
	row, err := loadTable(s, owner, id)
	if err != nil {
		return nil, err
	}
	schema, err := output.DecodeSchema([]byte(row.Schema))
	if err != nil {
		return nil, err
	}
	q, err = dataquery.Normalize(q, schema)
	if err != nil {
		return nil, invalid(err)
	}
	where, args, order, orderArgs := dataquery.SQL(q, schema)
	base := " FROM v22_table_records r WHERE r.table_id=?" + where
	params := append([]any{id}, args...)
	stats, err := s.QueryString(append([]any{"SELECT count(*)::text AS count,COALESCE(sum(octet_length(r.record_values::text)),0)::text AS bytes" + base}, params...)...)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(stats[0]["count"])
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(stats[0]["bytes"], 10, 64)
	if err != nil {
		return nil, err
	}
	if count > dataquery.MaxRows || size > dataquery.MaxBytes {
		return nil, ErrCapacity
	}
	sql := `SELECT r.id AS record_id,r.table_id::text,r.schema_version::text,r.revision::text,r.record_values::text AS values_json,r.first_observed_at::text,r.last_observed_at::text` + base + " ORDER BY " + order
	params = append(params, orderArgs...)
	rows, err := s.QueryString(append([]any{sql}, params...)...)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]string{}
	}
	raw, _ := json.Marshal(rows)
	if len(raw) > dataquery.MaxBytes {
		return nil, ErrCapacity
	}
	// Evict oldest unpinned snapshots beyond five per owner; exports pin their exact snapshot.
	if _, err = s.Exec(`DELETE FROM v22_data_snapshots WHERE id IN (SELECT p.id FROM v22_data_snapshots p WHERE p.owner_id=? AND NOT EXISTS(SELECT 1 FROM v22_exports e WHERE e.snapshot_id=p.id) ORDER BY p.captured_at DESC,p.id DESC OFFSET 4)`, owner); err != nil {
		return nil, err
	}
	queryJSON, _ := json.Marshal(q)
	now := time.Now().UTC()
	snapshot := &table.V22DataSnapshot{Id: uuid.NewString(), OwnerId: owner, TableId: id, Query: string(queryJSON), QueryHash: capture.Hash(queryJSON), Schema: row.Schema, SchemaVersion: row.SchemaVersion, Rows: string(raw), Count: count, CapturedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	if _, err = s.InsertOne(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}
func get(s *xorm.Session, owner, id int64, snapshotID string) (*table.V22DataSnapshot, error) {
	row := new(table.V22DataSnapshot)
	has, err := s.Where("id=? AND owner_id=? AND table_id=?", snapshotID, owner, id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrExpired
	}
	if !row.ExpiresAt.After(time.Now()) {
		return nil, ErrExpired
	}
	return row, nil
}

type Page struct {
	v22_data_repo.Page
	SnapshotID    string          `json:"snapshot_id"`
	CapturedAt    time.Time       `json:"captured_at"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Total         int             `json:"total"`
	Query         dataquery.Query `json:"query"`
	Schema        output.Schema   `json:"schema"`
	SchemaVersion int             `json:"schema_version"`
}

func Cursor(value string) (string, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "q" {
		return "", 0, v22_data_repo.ErrInvalid
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", 0, v22_data_repo.ErrInvalid
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 || offset > dataquery.MaxRows {
		return "", 0, v22_data_repo.ErrInvalid
	}
	return parts[1], offset, nil
}
func Records(ctx context.Context, owner, id int64, q dataquery.Query, cursor string, limit int) (Page, error) {
	page := Page{}
	if id < 1 || limit < 1 || limit > 50 {
		return page, v22_data_repo.ErrInvalid
	}
	s := db.Instance().NewSession().Context(quiet(ctx))
	defer s.Close()
	if err := s.Begin(); err != nil {
		return page, err
	}
	defer s.Rollback()
	var snap *table.V22DataSnapshot
	var err error
	offset := 0
	if cursor == "" {
		if _, err = s.Exec("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
			return page, err
		}
		snap, err = create(s, owner, id, q)
	} else {
		var snapshotID string
		snapshotID, offset, err = Cursor(cursor)
		if err == nil {
			if _, err = loadTable(s, owner, id); err != nil {
				return page, err
			}
			snap, err = get(s, owner, id, snapshotID)
		}
	}
	if err != nil {
		return page, err
	}
	schema, err := output.DecodeSchema([]byte(snap.Schema))
	if err != nil {
		return page, err
	}
	normalized, err := dataquery.Normalize(q, schema)
	if err != nil {
		return page, invalid(err)
	}
	raw, _ := json.Marshal(normalized)
	if capture.Hash(raw) != snap.QueryHash {
		return page, ErrConflict
	}
	rows := []map[string]string{}
	if err = json.Unmarshal([]byte(snap.Rows), &rows); err != nil {
		return page, err
	}
	if offset > len(rows) {
		return page, v22_data_repo.ErrInvalid
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	items := rows[offset:end]
	for _, row := range items {
		fields, err := v22_data_repo.ValueFields(row["values_json"], snap.Schema)
		if err != nil {
			return page, err
		}
		raw, _ := json.Marshal(fields)
		row["fields_json"] = string(raw)
	}
	page = Page{Page: v22_data_repo.Page{Items: items, More: end < len(rows)}, SnapshotID: snap.Id, CapturedAt: snap.CapturedAt, ExpiresAt: snap.ExpiresAt, Total: snap.Count, Query: normalized, Schema: schema, SchemaVersion: snap.SchemaVersion}
	if page.More {
		page.Next = fmt.Sprintf("q:%s:%d", snap.Id, end)
	}
	if err = s.Commit(); err != nil {
		return page, err
	}
	return page, nil
}
