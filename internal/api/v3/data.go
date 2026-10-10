package v3

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
)

func dataRead(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (any, error)) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, err := fn(ctx, admin.Id)
	if err != nil {
		switch {
		case errors.Is(err, v22_data_repo.ErrInvalid):
			fail(w, r, 400, "INVALID_ARGUMENT", "读取参数或游标无效", false, "data", "")
		case errors.Is(err, v22_data_repo.ErrNotFound):
			fail(w, r, 404, "NOT_FOUND", "数据、来源或分页锚点不存在", false, "data", "")
		case errors.Is(err, context.DeadlineExceeded):
			fail(w, r, 408, "DATA_TIMEOUT", "读取超时，请重试", true, "data", "")
		default:
			fail(w, r, 500, "INTERNAL", "数据读取异常", true, "storage", "")
		}
		return
	}
	ok(w, r, value)
}
func readLimit(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 25, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 50 {
		return 0, v22_data_repo.ErrInvalid
	}
	return n, nil
}
func TableRecords(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		id, err := strconv.ParseInt(mux.Vars(r)["table_id"], 10, 64)
		if err != nil || id < 1 {
			return nil, v22_data_repo.ErrInvalid
		}
		limit, err := readLimit(r)
		if err != nil {
			return nil, err
		}
		return v22_data_repo.Records(ctx, owner, id, r.URL.Query().Get("cursor"), limit)
	})
}
func TableStatistics(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		id, err := strconv.ParseInt(mux.Vars(r)["table_id"], 10, 64)
		if err != nil || id < 1 {
			return nil, v22_data_repo.ErrInvalid
		}
		return v22_data_repo.TableStats(ctx, owner, id)
	})
}
func RecordDetail(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		row, err := v22_data_repo.Record(ctx, owner, mux.Vars(r)["record_id"])
		if err != nil {
			return nil, err
		}
		fields, err := v22_data_repo.ValueFields(row["values_json"], row["schema_json"])
		if err != nil {
			return nil, err
		}
		return map[string]any{"record": row, "fields": fields}, nil
	})
}
func RecordObservations(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		limit, err := readLimit(r)
		if err != nil {
			return nil, err
		}
		return v22_data_repo.Observations(ctx, owner, mux.Vars(r)["record_id"], r.URL.Query().Get("cursor"), limit)
	})
}
func RecordRevisions(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		limit, err := readLimit(r)
		if err != nil {
			return nil, err
		}
		return v22_data_repo.Revisions(ctx, owner, mux.Vars(r)["record_id"], r.URL.Query().Get("cursor"), limit)
	})
}
func RunPages(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		limit, err := readLimit(r)
		if err != nil {
			return nil, err
		}
		return v22_data_repo.Pages(ctx, owner, mux.Vars(r)["run_id"], r.URL.Query().Get("cursor"), limit)
	})
}
func RunAttempts(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		rows, err := v22_data_repo.Attempts(ctx, owner, mux.Vars(r)["run_id"])
		return map[string]any{"scope": "run", "items": rows}, err
	})
}
func PageDetail(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		return v22_data_repo.PageDetail(ctx, owner, mux.Vars(r)["page_id"])
	})
}
func PageAttempts(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		row, err := v22_data_repo.PageDetail(ctx, owner, mux.Vars(r)["page_id"])
		if err != nil {
			return nil, err
		}
		rows, err := v22_data_repo.Attempts(ctx, owner, row["run_id"])
		return map[string]any{"scope": "run", "page_attempt": row["attempt"], "items": rows}, err
	})
}
func DocumentAsset(w http.ResponseWriter, r *http.Request) {
	dataRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		offset, limit := 0, 32768
		var err error
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, err = strconv.Atoi(v)
			if err != nil {
				return nil, v22_data_repo.ErrInvalid
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, err = strconv.Atoi(v)
			if err != nil {
				return nil, v22_data_repo.ErrInvalid
			}
		}
		return v22_data_repo.Document(ctx, owner, mux.Vars(r)["document_id"], offset, limit)
	})
}
