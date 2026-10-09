package v3

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/v22_capture_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_sample_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_session_repo"
	"github.com/nekoimi/scrapio/internal/sample"
)

func parseSampleBody(w http.ResponseWriter, r *http.Request, out any) error {
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON body required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 96*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("one JSON object required")
	}
	return nil
}
func sampleError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_sample_repo.ErrConflict):
		fail(w, r, 409, "SAMPLE_CONFLICT", "样例版本或请求键冲突，请重新读取；本地编辑仍保留", false, "sample", "")
	case errors.Is(err, v22_sample_repo.ErrProtected):
		fail(w, r, 409, "SAMPLE_PROTECTED", "请先取消保留保护，再删除样例", false, "sample", "")
	case errors.Is(err, v22_sample_repo.ErrNotFound) || errors.Is(err, v22_capture_repo.ErrNotFound) || errors.Is(err, v22_session_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "样例、输入或方案不存在", false, "sample", "")
	case errors.Is(err, v22_sample_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "sample", "")
	default:
		fail(w, r, 500, "INTERNAL", "样例存储异常，请保留本地修改并查询原请求", false, "storage", "")
	}
}
func sampleDTO(row *table.V22Sample) map[string]any {
	return map[string]any{"sample_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "capture_id": row.CaptureId, "name": row.Name, "stage": row.Stage, "step_id": row.StepId, "kind": row.Kind, "revision": row.Revision, "saved_revision": row.SavedRevision, "definition_hash": row.DefinitionHash, "expected": json.RawMessage(row.Expected), "expected_json": row.Expected, "expected_hash": row.ExpectedHash, "actions": json.RawMessage(row.Actions), "actions_truncated": row.ActionsTruncated, "protected": row.Protected, "input_retained": true, "screenshot_url": func() string {
		if row.ScreenshotHash == "" {
			return ""
		}
		return "/api/v3/samples/" + row.Id + "/screenshot"
	}(), "screenshot_hash": row.ScreenshotHash, "screenshot_policy": row.ScreenshotPolicy, "masks": json.RawMessage(row.Masks), "created_at": row.CreatedAt, "updated_at": row.UpdatedAt}
}
func checkDTO(row *table.V22SampleCheck, full bool) map[string]any {
	out := map[string]any{"check_id": row.Id, "sample_id": row.SampleId, "sample_revision": row.SampleRevision, "collector_revision": row.CollectorRevision, "definition_hash": row.DefinitionHash, "content_hash": row.ContentHash, "expected_hash": row.ExpectedHash, "interpreter_version": row.InterpreterVersion, "status": row.Status, "error_code": row.ErrorCode, "created_at": row.CreatedAt, "network_accessed": false, "dry_run": true}
	if full {
		out["result"] = json.RawMessage(row.Result)
		out["comparison"] = json.RawMessage(row.Comparison)
		out["expected_json"] = row.Expected
		out["definition_json"] = row.Definition
		out["error_message"] = row.ErrorMessage
	}
	return out
}
func CreateSample(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, authenticated := owner(r)
		if !authenticated {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		id, valid := collectorID(r)
		if !valid {
			fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "sample", "")
			return
		}
		var input v22_sample_repo.CreateInput
		if parseSampleBody(w, r, &input) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "样例配置无效或超出预算", false, "sample", "")
			return
		}
		if err := input.Validate(); err != nil {
			sampleError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len([]rune(key)) > 128 {
			fail(w, r, 400, "INVALID_ARGUMENT", "需要有效请求键", false, "sample", "")
			return
		}
		// Replay precedes any optional frame RPC, including after browser shutdown.
		existing, err := v22_sample_repo.Existing(admin.Id, id, key, input)
		if err != nil {
			sampleError(w, r, err)
			return
		}
		if existing != nil {
			created(w, r, sampleDTO(existing))
			return
		}
		var image []byte
		if input.IncludeScreenshot {
			source, err := v22_capture_repo.Get(admin.Id, input.CaptureID)
			if err != nil {
				sampleError(w, r, err)
				return
			}
			if source.CollectorId != id || source.Status != "succeeded" || source.Source != "browser" || source.ContentHash != capture.Hash([]byte(source.Content)) {
				fail(w, r, 400, "INVALID_ARGUMENT", "截图只能来自同方案的完整浏览器快照", false, "sample", "")
				return
			}
			session, err := v22_session_repo.Get(admin.Id, source.SessionId)
			if err != nil {
				sampleError(w, r, err)
				return
			}
			if session.CollectorId != id || session.Status != "ready" || session.PageStateId != source.PageStateId || !session.ExpiresAt.After(time.Now()) || browser == nil {
				fail(w, r, 409, "SCREENSHOT_UNAVAILABLE", "页面已变化或会话不可用；可取消截图后保存原快照样例", false, "sample", "")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			state, frameErr := browser.FrameEditorSession(ctx, session.Id, source.PageStateId)
			cancel()
			if frameErr != nil || state.PageStateID != source.PageStateId {
				fail(w, r, 409, "SCREENSHOT_UNAVAILABLE", "无法取得同页面版本截图；不会保存未确认图片", false, "sample", "")
				return
			}
			image, err = sample.RedactPNG(state.Screenshot, input.Masks)
			if err != nil {
				fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "sample", "")
				return
			}
		}
		row, err := v22_sample_repo.Create(admin.Id, id, key, input, image)
		if err != nil {
			sampleError(w, r, err)
			return
		}
		created(w, r, sampleDTO(row))
	}
}
func ListSamples(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "sample", "")
		return
	}
	if _, has, err := v22_collector_repo.Get(admin.Id, id); err != nil {
		sampleError(w, r, err)
		return
	} else if !has {
		sampleError(w, r, v22_sample_repo.ErrNotFound)
		return
	}
	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "limit 无效", false, "sample", "")
			return
		}
	}
	rows, next, err := v22_sample_repo.List(admin.Id, id, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		sampleError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range rows {
		items = append(items, sampleDTO(&rows[i]))
	}
	ok(w, r, map[string]any{"items": items, "next_cursor": next, "has_more": next != ""})
}
func loadOwnedSample(w http.ResponseWriter, r *http.Request) (*table.Admin, *table.V22Sample, bool) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return nil, nil, false
	}
	id := mux.Vars(r)["sample_id"]
	if _, err := uuid.Parse(id); err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "样例 ID 无效", false, "sample", "")
		return nil, nil, false
	}
	row, err := v22_sample_repo.Get(admin.Id, id)
	if err != nil {
		sampleError(w, r, err)
		return nil, nil, false
	}
	return admin, row, true
}
func GetSample(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	source, err := v22_capture_repo.Get(admin.Id, row.CaptureId)
	if err != nil {
		sampleError(w, r, err)
		return
	}
	dto := sampleDTO(row)
	dto["capture"] = captureDTO(source)
	ok(w, r, dto)
}
func GetSampleByKey(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	row, err := v22_sample_repo.GetByKey(admin.Id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		sampleError(w, r, err)
		return
	}
	ok(w, r, sampleDTO(row))
}
func UpdateSample(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	var input v22_sample_repo.UpdateInput
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "样例更新无效", false, "sample", "")
		return
	}
	updated, err := v22_sample_repo.Update(admin.Id, row.Id, input)
	if err != nil {
		sampleError(w, r, err)
		return
	}
	ok(w, r, sampleDTO(updated))
}
func DeleteSample(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("expected_revision"))
	if err != nil || revision < 1 {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要样例 expected_revision", false, "sample", "")
		return
	}
	if err = v22_sample_repo.Delete(admin.Id, row.Id, revision); err != nil {
		sampleError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"sample_id": row.Id, "deleted": true, "capture_deleted": false, "checks_deleted": true})
}
func GetSampleScreenshot(w http.ResponseWriter, r *http.Request) {
	_, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	if len(row.Screenshot) == 0 {
		sampleError(w, r, v22_sample_repo.ErrNotFound)
		return
	}
	if row.ScreenshotHash != capture.Hash(row.Screenshot) {
		fail(w, r, 409, "SCREENSHOT_CORRUPT", "图片校验失败", false, "sample", "")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(row.Screenshot)
}
func CheckSample(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	var input v22_sample_repo.CheckInput
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "检查请求无效", false, "sample", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	check, err := v22_sample_repo.Check(ctx, admin.Id, row.Id, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		sampleError(w, r, err)
		return
	}
	created(w, r, checkDTO(check, true))
}
func ListSampleChecks(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	checks, err := v22_sample_repo.Checks(admin.Id, row.Id)
	if err != nil {
		sampleError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range checks {
		items = append(items, checkDTO(&checks[i], false))
	}
	ok(w, r, map[string]any{"items": items, "retention_limit": 20})
}
func GetSampleCheck(w http.ResponseWriter, r *http.Request) {
	admin, row, loaded := loadOwnedSample(w, r)
	if !loaded {
		return
	}
	check, err := v22_sample_repo.GetCheck(admin.Id, row.Id, mux.Vars(r)["check_id"])
	if err != nil {
		sampleError(w, r, err)
		return
	}
	ok(w, r, checkDTO(check, true))
}
