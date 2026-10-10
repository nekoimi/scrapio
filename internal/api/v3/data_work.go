package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/dataquery"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_query_repo"
)

func dataWorkError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, v22_data_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "data", "")
	case errors.Is(err, v22_data_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "数据表、视图或导出不存在", false, "data", "")
	case errors.Is(err, v22_query_repo.ErrExpired):
		fail(w, r, 410, "ASSET_EXPIRED", "查询快照或导出已过期，请重新查询", false, "data", "")
	case errors.Is(err, v22_query_repo.ErrConflict):
		fail(w, r, 409, "DATA_CONFLICT", "筛选、视图版本或请求键冲突，请查询原结果", false, "data", "")
	case errors.Is(err, v22_query_repo.ErrCapacity):
		fail(w, r, 409, "DATA_CAPACITY", "超过查询/导出容量，请缩小筛选或等待任务保留期结束", false, "data", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		fail(w, r, 408, "DATA_TIMEOUT", "请求超时；写入请求请查询原结果", true, "data", "")
	default:
		fail(w, r, 500, "INTERNAL", "数据操作未确认，请查询原结果或重试读取", true, "storage", "")
	}
}
func dataOwner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return 0, false
	}
	return admin.Id, true
}
func dataTableID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(mux.Vars(r)["table_id"], 10, 64)
	if err != nil || id < 1 {
		return 0, v22_data_repo.ErrInvalid
	}
	return id, nil
}
func viewDTO(row table.V22DataView) any {
	return map[string]any{"view_id": row.Id, "table_id": strconv.FormatInt(row.TableId, 10), "name": row.Name, "revision": row.Revision, "query": json.RawMessage(row.Query), "created_at": row.CreatedAt, "updated_at": row.UpdatedAt}
}
func exportDTO(row *table.V22Export) any {
	status := row.Status
	if !row.ExpiresAt.After(time.Now()) {
		status = "expired"
	}
	return map[string]any{"export_id": row.Id, "table_id": strconv.FormatInt(row.TableId, 10), "format": row.Format, "status": status, "progress": row.Progress, "row_count": row.RowCount, "query": json.RawMessage(row.Query), "schema": json.RawMessage(row.Schema), "captured_at": row.CapturedAt, "created_at": row.CreatedAt, "finished_at": row.FinishedAt, "expires_at": row.ExpiresAt, "file_bytes": row.FileBytes, "file_hash": row.FileHash, "error_code": row.ErrorCode}
}
func DataViews(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	id, err := dataTableID(r)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := v22_query_repo.Views(ctx, owner, id)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	items := []any{}
	for _, row := range rows {
		items = append(items, viewDTO(row))
	}
	ok(w, r, map[string]any{"items": items, "limit": 30})
}
func SaveDataView(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	id, err := dataTableID(r)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	var input v22_query_repo.ViewInput
	if parseSampleBody(w, r, &input) != nil {
		dataWorkError(w, r, v22_data_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.SaveView(ctx, owner, id, input)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ok(w, r, viewDTO(*row))
}
func UpdateDataView(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	var input struct {
		Name             string          `json:"name"`
		ExpectedRevision int             `json:"expected_revision"`
		Query            json.RawMessage `json:"query"`
	}
	if parseSampleBody(w, r, &input) != nil {
		dataWorkError(w, r, v22_data_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.View(ctx, owner, mux.Vars(r)["view_id"])
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	var q v22_query_repo.ViewInput
	q.ID = row.Id
	q.Name = input.Name
	q.ExpectedRevision = input.ExpectedRevision
	var parseErr error
	q.Query, parseErr = dataquery.Decode(string(input.Query))
	if parseErr != nil {
		dataWorkError(w, r, v22_data_repo.ErrInvalid)
		return
	}
	saved, err := v22_query_repo.SaveView(ctx, owner, row.TableId, q)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ok(w, r, viewDTO(*saved))
}
func DeleteDataView(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("expected_revision"))
	if err != nil {
		dataWorkError(w, r, v22_data_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err = v22_query_repo.DeleteView(ctx, owner, mux.Vars(r)["view_id"], revision); err != nil {
		dataWorkError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"deleted": true})
}
func CreateDataExport(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	id, err := dataTableID(r)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	var input v22_query_repo.ExportInput
	if parseSampleBody(w, r, &input) != nil {
		dataWorkError(w, r, v22_data_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.CreateExport(ctx, owner, id, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"data": exportDTO(row), "request_id": w.Header().Get("X-Request-ID")})
}
func ListDataExports(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	id, err := dataTableID(r)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	limit, err := readLimit(r)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, more, err := v22_query_repo.Exports(ctx, owner, id, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	items := []any{}
	for i := range rows {
		items = append(items, exportDTO(&rows[i]))
	}
	next := ""
	if more {
		next = rows[len(rows)-1].Id
	}
	ok(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next})
}
func GetDataExport(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.Export(ctx, owner, mux.Vars(r)["export_id"], r.Header.Get("Idempotency-Key"))
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ok(w, r, exportDTO(row))
}
func CancelDataExport(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.CancelExport(ctx, owner, mux.Vars(r)["export_id"])
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	ok(w, r, exportDTO(row))
}
func DownloadDataExport(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_query_repo.Download(ctx, owner, mux.Vars(r)["export_id"])
	if err != nil {
		dataWorkError(w, r, err)
		return
	}
	mime := "application/json; charset=utf-8"
	if row.Format == "csv" {
		mime = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"scrapio-%s.%s\"", row.Id, row.Format))
	w.Header().Set("Content-Length", strconv.Itoa(len(row.File)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(row.File)
}
