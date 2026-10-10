package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_data_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_output_repo"
)

func outputError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_output_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "output", "")
	case errors.Is(err, v22_output_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "数据表、方案或输入不存在", false, "output", "")
	case errors.Is(err, v22_output_repo.ErrConflict):
		fail(w, r, 409, "OUTPUT_CONFLICT", "输入、规则、Schema 或请求键已变化，请重新预演；本地编辑保留", false, "output", "")
	case errors.Is(err, v22_output_repo.ErrBlocked):
		fail(w, r, 409, "OUTPUT_BLOCKED", "输出预演存在阻断项，不能确认保存", false, "output", "")
	default:
		fail(w, r, 500, "INTERNAL", "输出配置存储异常，请保留本地修改并查询原请求", false, "storage", "")
	}
}
func tableDTO(row *table.V22DataTable) map[string]any {
	return map[string]any{"table_id": strconv.FormatInt(row.Id, 10), "name": row.Name, "schema_version": row.SchemaVersion, "schema": json.RawMessage(row.Schema), "schema_hash": row.SchemaHash, "created_at": row.CreatedAt}
}
func outputCheckDTO(row *table.V22OutputCheck) map[string]any {
	return map[string]any{"check_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "collector_revision": row.CollectorRevision, "definition_hash": row.DefinitionHash, "capture_id": row.CaptureId, "content_hash": row.ContentHash, "sample_id": row.SampleId, "sample_revision": row.SampleRevision, "table_id": func() string {
		if row.TableId == 0 {
			return ""
		}
		return strconv.FormatInt(row.TableId, 10)
	}(), "schema_version": row.SchemaVersion, "schema_hash": row.SchemaHash, "config": json.RawMessage(row.Config), "result": json.RawMessage(row.Result), "ready": row.Ready, "expires_at": row.ExpiresAt, "created_at": row.CreatedAt}
}
func CreateDataTable(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	var input v22_output_repo.TableInput
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "数据表配置无效", false, "output", "")
		return
	}
	row, err := v22_output_repo.CreateTable(admin.Id, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		outputError(w, r, err)
		return
	}
	created(w, r, tableDTO(row))
}
func ListDataTables(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	limit := 25
	before := int64(0)
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := r.URL.Query().Get("cursor"); value != "" {
			before, err = strconv.ParseInt(value, 10, 64)
		}
	}
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "output", "")
		return
	}
	rows, next, err := v22_output_repo.ListTables(admin.Id, before, limit)
	if err != nil {
		outputError(w, r, err)
		return
	}
	ids := []int64{}
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	stats, err := v22_data_repo.Statistics(ctx, admin.Id, ids)
	if err != nil {
		outputError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range rows {
		item := tableDTO(&rows[i])
		item["statistics"] = stats[strconv.FormatInt(rows[i].Id, 10)]
		items = append(items, item)
	}
	ok(w, r, map[string]any{"items": items, "next_cursor": next, "has_more": next != ""})
}
func GetDataTable(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["table_id"], 10, 64)
	if err != nil || id < 1 {
		fail(w, r, 400, "INVALID_ARGUMENT", "数据表 ID 无效", false, "output", "")
		return
	}
	row, err := v22_output_repo.GetTable(admin.Id, id)
	if err != nil {
		outputError(w, r, err)
		return
	}
	ok(w, r, tableDTO(row))
}
func CreateOutputCheck(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "output", "")
		return
	}
	var input v22_output_repo.CheckInput
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "输出预演配置无效", false, "output", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	row, err := v22_output_repo.CreateCheck(ctx, admin.Id, id, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		outputError(w, r, err)
		return
	}
	created(w, r, outputCheckDTO(row))
}
func GetOutputCheck(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "output", "")
		return
	}
	key := ""
	checkID := mux.Vars(r)["check_id"]
	if checkID == "" {
		key = r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 512 {
			fail(w, r, 400, "INVALID_ARGUMENT", "请求键无效", false, "output", "")
			return
		}
	}
	row, err := v22_output_repo.GetCheck(admin.Id, id, checkID, key)
	if err != nil {
		outputError(w, r, err)
		return
	}
	ok(w, r, outputCheckDTO(row))
}
func ConfirmOutput(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "output", "")
		return
	}
	var input v22_output_repo.ConfirmInput
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "输出确认配置无效", false, "output", "")
		return
	}
	row, err := v22_output_repo.Confirm(admin.Id, id, input)
	if err != nil {
		outputError(w, r, err)
		return
	}
	ok(w, r, v22_collector_repo.ToDTO(row))
}
