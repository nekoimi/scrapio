// Package v22_data_repo reads only the fresh v2.2 data domain.
package v22_data_repo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db"
)

var ErrNotFound = errors.New("data not found")
var ErrInvalid = errors.New("invalid data read arguments")

type Row = map[string]string
type Page struct {
	Items []Row  `json:"items"`
	Next  string `json:"next_cursor"`
	More  bool   `json:"has_more"`
}

func query(ctx context.Context, sql string, args ...any) ([]Row, error) {
	s := db.Instance().NewSession().Context(ctx)
	defer s.Close()
	rows, err := s.QueryString(append([]any{sql}, args...)...)
	if rows == nil {
		rows = []Row{}
	}
	return rows, err
}
func one(ctx context.Context, sql string, args ...any) (Row, error) {
	rows, err := query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}
func validID(id string) bool { _, err := uuid.Parse(id); return err == nil }
func paged(rows []Row, limit int, key string) Page {
	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		page.More = true
		page.Next = page.Items[limit-1][key]
	}
	return page
}

// Cursor anchors are looked up in the same authorized scope. Ordering is immutable.
func Records(ctx context.Context, owner, tableID int64, cursor string, limit int) (Page, error) {
	if tableID < 1 || limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := one(ctx, "SELECT id FROM v22_data_tables WHERE id=? AND owner_id=?", tableID, owner); err != nil {
		return Page{}, err
	}
	clause := ""
	args := []any{tableID, owner}
	if cursor != "" {
		if !validID(cursor) {
			return Page{}, ErrInvalid
		}
		anchor, err := one(ctx, "SELECT first_observed_at::text AS stamp,id FROM v22_table_records WHERE table_id=? AND id=?", tableID, cursor)
		if err != nil {
			return Page{}, err
		}
		clause = " AND (r.first_observed_at,r.id)<(?::timestamptz,?)"
		args = append(args, anchor["stamp"], anchor["id"])
	}
	args = append(args, limit+1)
	rows, err := query(ctx, `SELECT r.id AS record_id,r.table_id::text,r.schema_version::text,r.revision::text,r.record_values::text AS values_json,r.first_observed_at::text,r.last_observed_at::text,s.schema::text AS schema_json FROM v22_table_records r JOIN v22_data_tables t ON t.id=r.table_id JOIN v22_data_table_schemas s ON s.table_id=r.table_id AND s.version=r.schema_version WHERE r.table_id=? AND t.owner_id=?`+clause+" ORDER BY r.first_observed_at DESC,r.id DESC LIMIT ?", args...)
	if err != nil {
		return Page{}, err
	}
	for _, row := range rows {
		fields, e := ValueFields(row["values_json"], row["schema_json"])
		if e != nil {
			return Page{}, e
		}
		raw, _ := json.Marshal(fields)
		row["fields_json"] = string(raw)
		delete(row, "schema_json")
	}
	return paged(rows, limit, "record_id"), nil
}
func Record(ctx context.Context, owner int64, id string) (Row, error) {
	if !validID(id) {
		return nil, ErrInvalid
	}
	return one(ctx, `SELECT r.id AS record_id,r.table_id::text,t.name AS table_name,r.schema_version::text,r.revision::text,r.record_values::text AS values_json,r.key_values::text AS key_json,r.first_observed_at::text,r.last_observed_at::text,s.schema::text AS schema_json FROM v22_table_records r JOIN v22_data_tables t ON t.id=r.table_id JOIN v22_data_table_schemas s ON s.table_id=r.table_id AND s.version=r.schema_version WHERE r.id=? AND t.owner_id=?`, id, owner)
}
func Observations(ctx context.Context, owner int64, id, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := Record(ctx, owner, id); err != nil {
		return Page{}, err
	}
	clause := ""
	args := []any{id, owner, owner}
	if cursor != "" {
		if !validID(cursor) {
			return Page{}, ErrInvalid
		}
		a, err := one(ctx, "SELECT observed_at::text AS stamp,id FROM v22_table_observations WHERE record_id=? AND id=?", id, cursor)
		if err != nil {
			return Page{}, err
		}
		clause = " AND (o.observed_at,o.id)<(?::timestamptz,?)"
		args = append(args, a["stamp"], a["id"])
	}
	args = append(args, limit+1)
	rows, err := query(ctx, `SELECT o.id AS observation_id,o.record_id,o.schema_version::text,o.collector_id::text,o.run_id,o.version_id,o.document_id AS page_id,o.source_url,o.page_role,o.outcome,o.observed_at::text,o.observed_values::text AS values_json,u.version_number::text FROM v22_table_observations o JOIN v22_data_tables t ON t.id=o.table_id JOIN v22_runs u ON u.id=o.run_id WHERE o.record_id=? AND t.owner_id=? AND u.owner_id=?`+clause+" ORDER BY o.observed_at DESC,o.id DESC LIMIT ?", args...)
	return paged(rows, limit, "observation_id"), err
}
func Revisions(ctx context.Context, owner int64, id, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := Record(ctx, owner, id); err != nil {
		return Page{}, err
	}
	clause := ""
	args := []any{id, owner}
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 1 {
			return Page{}, ErrInvalid
		}
		if _, err = one(ctx, "SELECT revision FROM v22_table_record_revisions WHERE record_id=? AND revision=?", id, n); err != nil {
			return Page{}, err
		}
		clause = " AND v.revision<?"
		args = append(args, n)
	}
	args = append(args, limit+1)
	rows, err := query(ctx, `SELECT v.revision::text,v.schema_version::text,v.record_values::text AS values_json,v.changed_fields::text AS changed_fields_json,v.observation_id,v.created_at::text,s.schema::text AS schema_json,p.record_values::text AS previous_values_json FROM v22_table_record_revisions v JOIN v22_table_records r ON r.id=v.record_id JOIN v22_data_tables t ON t.id=r.table_id JOIN v22_data_table_schemas s ON s.table_id=r.table_id AND s.version=v.schema_version LEFT JOIN v22_table_record_revisions p ON p.record_id=v.record_id AND p.revision=v.revision-1 WHERE v.record_id=? AND t.owner_id=?`+clause+" ORDER BY v.revision DESC LIMIT ?", args...)
	return paged(rows, limit, "revision"), err
}
func TableStats(ctx context.Context, owner, tableID int64) (Row, error) {
	stats, err := Statistics(ctx, owner, []int64{tableID})
	if err != nil {
		return nil, err
	}
	row := stats[strconv.FormatInt(tableID, 10)]
	if row == nil {
		return nil, ErrNotFound
	}
	return row, nil
}

func Pages(ctx context.Context, owner int64, runID, cursor string, limit int) (Page, error) {
	if !validID(runID) || limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := one(ctx, "SELECT id FROM v22_runs WHERE id=? AND owner_id=?", runID, owner); err != nil {
		return Page{}, err
	}
	clause := ""
	args := []any{runID, owner}
	if cursor != "" {
		if !validID(cursor) {
			return Page{}, ErrInvalid
		}
		a, err := one(ctx, "SELECT sequence FROM v22_run_documents WHERE run_id=? AND id=?", runID, cursor)
		if err != nil {
			return Page{}, err
		}
		clause = " AND d.sequence>?"
		n, err := strconv.ParseInt(a["sequence"], 10, 64)
		if err != nil {
			return Page{}, err
		}
		args = append(args, n)
	}
	args = append(args, limit+1)
	rows, err := query(ctx, `SELECT d.id AS page_id,d.run_id,d.attempt::text,d.sequence::text,(d.result-'extraction')::text AS metadata_json,CASE WHEN d.content='' THEN 'unavailable' ELSE 'stored' END AS evidence_status FROM v22_run_documents d JOIN v22_runs r ON r.id=d.run_id WHERE d.run_id=? AND r.owner_id=?`+clause+" ORDER BY d.sequence LIMIT ?", args...)
	return paged(rows, limit, "page_id"), err
}
func PageDetail(ctx context.Context, owner int64, id string) (Row, error) {
	if !validID(id) {
		return nil, ErrInvalid
	}
	return one(ctx, `SELECT d.id AS page_id,d.run_id,d.attempt::text,d.sequence::text,d.result::text AS result_json,r.version_id,r.version_number::text,r.collector_id::text,CASE WHEN d.content='' THEN 'unavailable' ELSE 'stored' END AS evidence_status,(SELECT p.id FROM v22_run_documents p WHERE p.run_id=d.run_id AND p.attempt=d.attempt AND p.result->>'stage'='list' AND p.result->>'step_id'=d.result->>'step_id' AND p.result->>'list_page'=d.result->>'list_page' AND d.result->>'stage'='detail' ORDER BY p.sequence LIMIT 1) AS parent_page_id FROM v22_run_documents d JOIN v22_runs r ON r.id=d.run_id WHERE d.id=? AND r.owner_id=?`, id, owner)
}
func Attempts(ctx context.Context, owner int64, runID string) ([]Row, error) {
	if !validID(runID) {
		return nil, ErrInvalid
	}
	if _, err := one(ctx, "SELECT id FROM v22_runs WHERE id=? AND owner_id=?", runID, owner); err != nil {
		return nil, err
	}
	return query(ctx, `SELECT a.attempt::text,a.status,a.started_at::text,a.finished_at::text,a.summary::text AS summary_json FROM v22_run_attempts a JOIN v22_runs r ON r.id=a.run_id WHERE a.run_id=? AND r.owner_id=? ORDER BY a.attempt LIMIT 50`, runID, owner)
}

type Asset struct {
	DocumentID string `json:"document_id"`
	Status     string `json:"status"`
	Content    string `json:"content"`
	Hash       string `json:"content_hash"`
	Total      int    `json:"total_bytes"`
	Offset     int    `json:"offset"`
	Next       int    `json:"next_offset"`
	More       bool   `json:"has_more"`
}

// Byte offsets must be UTF-8 boundaries; returned chunks never split a rune.
func Chunk(content, hash string, offset, limit int) (Asset, error) {
	a := Asset{Status: "available", Hash: hash, Total: len(content), Offset: offset, Next: offset}
	if offset < 0 || offset > len(content) || limit < 4 || limit > 65536 {
		return a, ErrInvalid
	}
	if content == "" {
		a.Status = "unavailable"
		return a, nil
	}
	if !utf8.ValidString(content) || capture.Hash([]byte(content)) != hash {
		a.Status = "corrupt"
		return a, nil
	}
	if offset < len(content) && !utf8.RuneStart(content[offset]) {
		return a, ErrInvalid
	}
	end := offset + limit
	if end > len(content) {
		end = len(content)
	}
	for end < len(content) && !utf8.RuneStart(content[end]) {
		end--
	}
	a.Content = content[offset:end]
	a.Next = end
	a.More = end < len(content)
	return a, nil
}
func Document(ctx context.Context, owner int64, id string, offset, limit int) (Asset, error) {
	if !validID(id) {
		return Asset{}, ErrInvalid
	}
	row, err := one(ctx, `SELECT d.content,d.result->>'content_hash' AS hash FROM v22_run_documents d JOIN v22_runs r ON r.id=d.run_id WHERE d.id=? AND r.owner_id=?`, id, owner)
	if err != nil {
		return Asset{}, err
	}
	a, err := Chunk(row["content"], row["hash"], offset, limit)
	a.DocumentID = id
	return a, err
}

// Values remain serialized JSON strings so JavaScript never rounds large numbers.
func ValueFields(values, schema string) ([]map[string]string, error) {
	var v map[string]json.RawMessage
	var s struct {
		Fields []struct {
			Key  string `json:"field_key"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if json.Unmarshal([]byte(values), &v) != nil || json.Unmarshal([]byte(schema), &s) != nil {
		return nil, errors.New("corrupt values/schema")
	}
	fields := []map[string]string{}
	for _, f := range s.Fields {
		raw := v[f.Key]
		if len(raw) == 0 {
			raw = json.RawMessage("null")
		}
		fields = append(fields, map[string]string{"field_key": f.Key, "name": f.Name, "type": f.Type, "value_json": string(raw)})
	}
	return fields, nil
}

// Statistics for one bounded table-list page in a single round trip.
func Statistics(ctx context.Context, owner int64, ids []int64) (map[string]Row, error) {
	result := map[string]Row{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 50 {
		return nil, ErrInvalid
	}
	placeholders := ""
	args := []any{owner}
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := query(ctx, `SELECT t.id::text AS table_id,(SELECT count(*)::text FROM v22_table_records r WHERE r.table_id=t.id) AS record_count,(SELECT max(last_observed_at)::text FROM v22_table_records r WHERE r.table_id=t.id) AS last_observed_at,(SELECT max(v.created_at)::text FROM v22_table_record_revisions v JOIN v22_table_records r ON r.id=v.record_id WHERE r.table_id=t.id) AS last_changed_at,COALESCE((SELECT jsonb_agg(x)::text FROM (SELECT DISTINCT o.collector_id::text AS collector_id,c.name FROM v22_table_observations o JOIN v22_collectors c ON c.id=o.collector_id WHERE o.table_id=t.id AND c.owner_id=t.owner_id) x),'[]') AS collectors_json FROM v22_data_tables t WHERE t.owner_id=? AND t.id IN (`+placeholders+`)`, args...)
	for _, row := range rows {
		result[row["table_id"]] = row
	}
	return result, err
}
