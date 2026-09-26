package record_repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
)

type Filter struct {
	DatasetID int64      `json:"dataset_id"`
	Query     string     `json:"query,omitempty"`
	SourceID  int64      `json:"source_id,omitempty"`
	Activity  string     `json:"activity,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
}

func (f Filter) Validate() error {
	if f.DatasetID <= 0 {
		return errors.New("dataset_id is required")
	}
	if len([]rune(f.Query)) > 200 {
		return errors.New("query is too long")
	}
	if f.SourceID < -1 {
		return errors.New("invalid source_id")
	}
	if f.Activity != "" && f.Activity != "created" && f.Activity != "updated" {
		return errors.New("activity must be created or updated")
	}
	if f.Since != nil && (f.Since.After(time.Now().Add(time.Minute)) || f.Since.Before(time.Now().AddDate(-20, 0, 0))) {
		return errors.New("since is out of range")
	}
	return nil
}

func filterWhere(f Filter) (string, []any) {
	args := []any{f.DatasetID}
	where := "r.dataset_id=$1 AND r.status='active'"
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), `%`, `\%`), `_`, `\_`)+"%")
		where += fmt.Sprintf(" AND (r.canonical_key ILIKE $%d OR r.normalized::text ILIKE $%d)", len(args), len(args))
	}
	if f.SourceID > 0 {
		args = append(args, f.SourceID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM record_observations o WHERE o.record_id=r.id AND o.source_id=$%d)", len(args))
	} else if f.SourceID == -1 {
		where += " AND EXISTS (SELECT 1 FROM record_observations o WHERE o.record_id=r.id AND o.source_id IS NULL)"
	}
	if f.Activity != "" {
		args = append(args, f.Activity)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM record_revisions rv JOIN record_observations o ON o.id=rv.observation_id WHERE rv.record_id=r.id AND o.decision=$%d", len(args))
		if f.Since != nil {
			args = append(args, *f.Since)
			where += fmt.Sprintf(" AND rv.created_at >= $%d", len(args))
		}
		where += ")"
	} else if f.Since != nil {
		args = append(args, *f.Since)
		where += fmt.Sprintf(" AND r.last_seen_at >= $%d", len(args))
	}
	return where, args
}

type ListRow struct {
	table.Record
	LastDecision  string     `json:"last_decision"`
	LastChangedAt *time.Time `json:"last_changed_at,omitempty"`
	SourceCount   int64      `json:"source_count"`
}

func ListFiltered(f Filter, page, size int) ([]ListRow, int64, error) {
	if err := f.Validate(); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	if page > 100000 {
		return nil, 0, errors.New("page is too large")
	}
	where, args := filterWhere(f)
	conn := db.Instance().DB().DB
	var total int64
	if err := conn.QueryRow("SELECT COUNT(*) FROM records r WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := conn.Query(`SELECT r.id,r.dataset_id,r.canonical_key,r.normalized,r.status,r.first_seen_at,r.last_seen_at,r.created_at,r.updated_at,
change.decision,change.created_at,(SELECT COUNT(DISTINCT o.source_id) FROM record_observations o WHERE o.record_id=r.id)
FROM records r LEFT JOIN LATERAL (SELECT o.decision,rv.created_at FROM record_revisions rv
JOIN record_observations o ON o.id=rv.observation_id WHERE rv.record_id=r.id
ORDER BY rv.created_at DESC,rv.id DESC LIMIT 1) change ON true WHERE `+where+" ORDER BY r.last_seen_at DESC,r.id DESC LIMIT "+fmt.Sprint(size)+" OFFSET "+fmt.Sprint((page-1)*size), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]ListRow, 0)
	for rows.Next() {
		var row ListRow
		var normalized []byte
		var decision sql.NullString
		var changed sql.NullTime
		if err := rows.Scan(&row.Id, &row.DatasetId, &row.CanonicalKey, &normalized, &row.Status, &row.FirstSeenAt, &row.LastSeenAt, &row.CreatedAt, &row.UpdatedAt, &decision, &changed, &row.SourceCount); err != nil {
			return nil, 0, err
		}
		row.LastDecision = decision.String
		if changed.Valid {
			row.LastChangedAt = &changed.Time
		}
		row.Normalized = string(normalized)
		out = append(out, row)
	}
	return out, total, rows.Err()
}

type SourceCoverage struct {
	SourceID       int64     `json:"source_id"`
	SourceName     string    `json:"source_name"`
	Records        int64     `json:"records"`
	Observations   int64     `json:"observations"`
	LastObservedAt time.Time `json:"last_observed_at"`
}

func Coverage(datasetID int64) ([]SourceCoverage, error) {
	if datasetID <= 0 {
		return nil, errors.New("dataset_id is required")
	}
	rows, err := db.Instance().DB().DB.Query(`SELECT COALESCE(o.source_id,0),COALESCE(s.name,'未标识来源'),COUNT(DISTINCT o.record_id),COUNT(*),MAX(o.observed_at)
FROM record_observations o LEFT JOIN sources s ON s.id=o.source_id WHERE o.dataset_id=$1
GROUP BY o.source_id,s.name ORDER BY COUNT(DISTINCT o.record_id) DESC`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SourceCoverage, 0)
	for rows.Next() {
		var row SourceCoverage
		if err := rows.Scan(&row.SourceID, &row.SourceName, &row.Records, &row.Observations, &row.LastObservedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type SavedView struct {
	ID        int64  `json:"id"`
	DatasetID int64  `json:"dataset_id"`
	Name      string `json:"name"`
	Filter    Filter `json:"filter"`
}

func ListViews(datasetID int64) ([]SavedView, error) {
	if datasetID <= 0 {
		return nil, errors.New("dataset_id is required")
	}
	rows, err := db.Instance().DB().DB.Query("SELECT id,name,filter FROM record_views WHERE dataset_id=$1 ORDER BY name", datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SavedView, 0)
	for rows.Next() {
		var v SavedView
		var raw []byte
		v.DatasetID = datasetID
		if err := rows.Scan(&v.ID, &v.Name, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Filter); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func SaveView(datasetID int64, name string, f Filter) (*SavedView, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 80 {
		return nil, errors.New("view name must be 1–80 characters")
	}
	f.DatasetID = datasetID
	if err := f.Validate(); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(f)
	v := &SavedView{DatasetID: datasetID, Name: name, Filter: f}
	err := db.Instance().DB().DB.QueryRow(`INSERT INTO record_views(dataset_id,name,filter) VALUES($1,$2,$3::jsonb)
ON CONFLICT(dataset_id,name) DO UPDATE SET filter=EXCLUDED.filter,updated_at=NOW() RETURNING id`, datasetID, name, string(raw)).Scan(&v.ID)
	return v, err
}
func DeleteView(datasetID, id int64) error {
	if datasetID <= 0 || id <= 0 {
		return errors.New("invalid view")
	}
	_, err := db.Instance().DB().DB.Exec("DELETE FROM record_views WHERE id=$1 AND dataset_id=$2", id, datasetID)
	return err
}

func CountFiltered(ctx context.Context, f Filter) (int64, error) {
	if err := f.Validate(); err != nil {
		return 0, err
	}
	where, args := filterWhere(f)
	var n int64
	err := db.Instance().DB().DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM records r WHERE "+where, args...).Scan(&n)
	return n, err
}

func queryExportRows(ctx context.Context, tx *sql.Tx, f Filter) (*sql.Rows, error) {
	where, args := filterWhere(f)
	return tx.QueryContext(ctx, "SELECT r.canonical_key,r.normalized,r.first_seen_at,r.last_seen_at FROM records r WHERE "+where+" ORDER BY r.last_seen_at DESC,r.id DESC", args...)
}
