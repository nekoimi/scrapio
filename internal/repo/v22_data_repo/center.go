package v22_data_repo

import (
	"context"
	"strconv"
)

func AttemptsPage(ctx context.Context, owner int64, runID, cursor string, limit int) (Page, error) {
	if !validID(runID) || limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := one(ctx, "SELECT id FROM v22_runs WHERE id=? AND owner_id=?", runID, owner); err != nil {
		return Page{}, err
	}
	after := 0
	if cursor != "" {
		var err error
		after, err = strconv.Atoi(cursor)
		if err != nil || after < 1 {
			return Page{}, ErrInvalid
		}
		if _, err = one(ctx, "SELECT attempt FROM v22_run_attempts WHERE run_id=? AND attempt=?", runID, after); err != nil {
			return Page{}, err
		}
	}
	rows, err := query(ctx, `SELECT a.attempt::text,a.status,a.started_at::text,a.finished_at::text,a.summary::text AS summary_json FROM v22_run_attempts a JOIN v22_runs r ON r.id=a.run_id WHERE a.run_id=? AND r.owner_id=? AND a.attempt>? ORDER BY a.attempt LIMIT ?`, runID, owner, after, limit+1)
	return paged(rows, limit, "attempt"), err
}

// Diagnostics are the durable journal, including operations without a captured page.
func Diagnostics(ctx context.Context, owner int64, runID, cursor string, limit int) (Page, error) {
	if !validID(runID) || limit < 1 || limit > 50 {
		return Page{}, ErrInvalid
	}
	if _, err := one(ctx, "SELECT id FROM v22_runs WHERE id=? AND owner_id=?", runID, owner); err != nil {
		return Page{}, err
	}
	before := int64(9223372036854775807)
	if cursor != "" {
		var err error
		before, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || before < 1 {
			return Page{}, ErrInvalid
		}
		if _, err = one(ctx, "SELECT sequence FROM v22_run_events WHERE run_id=? AND sequence=?", runID, before); err != nil {
			return Page{}, err
		}
	}
	rows, err := query(ctx, `SELECT e.sequence::text,e.attempt::text,e.created_at::text,e.payload::text AS event_json FROM v22_run_events e JOIN v22_runs r ON r.id=e.run_id WHERE e.run_id=? AND r.owner_id=? AND e.sequence<? ORDER BY e.sequence DESC LIMIT ?`, runID, owner, before, limit+1)
	return paged(rows, limit, "sequence"), err
}

func Coverage(ctx context.Context, owner int64, runID string) (Row, error) {
	if !validID(runID) {
		return nil, ErrInvalid
	}
	// Document counters include intermediate list/detail records and duplicates.
	return one(ctx, `SELECT r.id AS run_id,
 (SELECT count(*)::text FROM v22_run_documents d WHERE d.run_id=r.id) AS captured_documents,
 (SELECT count(*)::text FROM v22_run_documents d CROSS JOIN LATERAL jsonb_array_elements(d.result->'extraction'->'records') AS x(record) WHERE d.run_id=r.id) AS extracted_records,
 (SELECT count(*)::text FROM v22_run_documents d CROSS JOIN LATERAL jsonb_array_elements(d.result->'extraction'->'records') AS x(record) WHERE d.run_id=r.id AND x.record->>'valid'='true') AS valid_records,
 (SELECT count(*)::text FROM v22_run_documents d CROSS JOIN LATERAL jsonb_array_elements(d.result->'extraction'->'records') AS x(record) WHERE d.run_id=r.id AND x.record->>'valid'='false') AS invalid_records
 FROM v22_runs r WHERE r.id=? AND r.owner_id=?`, runID, owner)
}
