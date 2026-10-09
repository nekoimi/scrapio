package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_capture_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
)

func captureDTO(row *table.V22Capture) map[string]any {
	var networkAccessed any = row.NetworkAccessed
	if row.Source == "http" && !row.NetworkAccessed && (row.Status == "running" || row.ErrorStage == "recovery") {
		networkAccessed = nil
	}
	return map[string]any{"capture_id": row.Id, "session_id": row.SessionId, "page_state_id": row.PageStateId, "base_url": row.BaseURL, "collector_id": strconv.FormatInt(row.CollectorId, 10), "draft_revision": row.DraftRevision, "source": row.Source, "format": row.Format, "status": row.Status, "final_url": row.FinalURL, "status_code": row.StatusCode, "content_type": row.ContentType, "content": row.Content, "content_hash": row.ContentHash, "byte_count": row.ByteCount, "error_code": row.ErrorCode, "error_stage": row.ErrorStage, "created_at": row.CreatedAt, "deadline_at": row.DeadlineAt, "network_accessed": networkAccessed, "dry_run": true, "sanitized": true}
}
func captureError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_capture_repo.ErrConflict):
		fail(w, r, 409, "IDEMPOTENCY_CONFLICT", err.Error(), false, "capture", "")
	case errors.Is(err, v22_capture_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", err.Error(), false, "capture", "")
	default:
		fail(w, r, 500, "INTERNAL", "快照存储失败，请查询原请求；不会自动重发网络请求", false, "storage", "")
	}
}
func CreateCapture(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, authenticated := owner(r)
		if !authenticated {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		var input capture.Input
		r.Body = http.MaxBytesReader(w, r.Body, capture.MaxBytes+64*1024)
		if err := request.Parse(r, &input); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "输入内容无效或超过上限", false, "capture", "")
			return
		}
		if err := input.Validate(); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "capture", "")
			return
		}
		if input.Source == "browser" {
			fail(w, r, 400, "INVALID_ARGUMENT", "浏览器快照请使用会话 captures 接口", false, "capture", "")
			return
		}
		id, err := strconv.ParseInt(input.CollectorID, 10, 64)
		if err != nil || id < 1 {
			fail(w, r, 400, "INVALID_ARGUMENT", "collector_id 无效", false, "capture", "")
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len([]rune(key)) > 128 {
			fail(w, r, 400, "INVALID_ARGUMENT", "Idempotency-Key 必填且最多 128 字符", false, "capture", "")
			return
		}
		collector, has, err := v22_collector_repo.Get(admin.Id, id)
		if err != nil {
			captureError(w, r, err)
			return
		}
		if !has {
			captureError(w, r, v22_capture_repo.ErrNotFound)
			return
		}
		var definition struct {
			HTTPRequest json.RawMessage `json:"http_request"`
		}
		if err = json.Unmarshal([]byte(collector.Definition), &definition); err != nil {
			captureError(w, r, err)
			return
		}
		if _, err = capture.ValidateURL(collector.EntryURL); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "capture", "entry_url")
			return
		}
		httpRequest := capture.HTTPRequest{}
		if input.Source == "http" {
			data := definition.HTTPRequest
			if len(data) == 0 {
				data = []byte("{}")
			}
			httpRequest, err = capture.DecodeRequest(data)
		}
		if err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "capture", "http_request")
			return
		}
		row, dispatch, err := v22_capture_repo.Create(admin.Id, collector, key, input, httpRequest)
		if err != nil {
			captureError(w, r, err)
			return
		}
		if dispatch {
			var entryCfg *config.HTTPEntryConfig
			if cfg != nil {
				entryCfg = cfg.HTTPEntry
			}
			allowPrivate := entryCfg != nil && entryCfg.AllowPrivateNetwork
			var result capture.Result
			header, value, secret, credentialErr := "", "", "", error(nil)
			if input.Source == "http" {
				header, value, secret, credentialErr = capture.Credential(entryCfg, admin.Id, collector.EntryURL, httpRequest.CredentialRef)
			}
			if input.Source == "http" && credentialErr != nil {
				result = capture.Result{Status: "failed", ErrorCode: "CREDENTIAL_UNAVAILABLE", ErrorStage: "credential"}
			} else {
				result = capture.Execute(r.Context(), capture.NewClient(allowPrivate), input, httpRequest, collector.EntryURL, header, value, secret)
			}
			if err = v22_capture_repo.Complete(row, result); err != nil {
				captureError(w, r, err)
				return
			}
		}
		row, err = v22_capture_repo.Get(admin.Id, row.Id)
		if err != nil {
			captureError(w, r, err)
			return
		}
		created(w, r, captureDTO(row))
	}
}
func GetCapture(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id := mux.Vars(r)["capture_id"]
	if _, err := uuid.Parse(id); err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "capture_id 无效", false, "capture", "")
		return
	}
	row, err := v22_capture_repo.Get(admin.Id, id)
	if err != nil {
		captureError(w, r, err)
		return
	}
	ok(w, r, captureDTO(row))
}
func GetCaptureByKey(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len([]rune(key)) > 128 {
		fail(w, r, 400, "INVALID_ARGUMENT", "请求键无效", false, "capture", "")
		return
	}
	row, err := v22_capture_repo.GetByKey(admin.Id, key)
	if err != nil {
		captureError(w, r, err)
		return
	}
	ok(w, r, captureDTO(row))
}
func CheckCaptureJSON(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	row, err := v22_capture_repo.Get(admin.Id, mux.Vars(r)["capture_id"])
	if err != nil {
		captureError(w, r, err)
		return
	}
	var input struct {
		ExpectedRevision int    `json:"expected_revision"`
		StepID           string `json:"step_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if request.Parse(r, &input) != nil || input.ExpectedRevision < 1 || input.StepID == "" {
		fail(w, r, 400, "INVALID_ARGUMENT", "revision 和 step_id 必填", false, "json-check", "")
		return
	}
	collector, has, err := v22_collector_repo.Get(admin.Id, row.CollectorId)
	if err != nil {
		captureError(w, r, err)
		return
	}
	if !has || collector.Status == "archived" {
		captureError(w, r, v22_capture_repo.ErrNotFound)
		return
	}
	if collector.Revision != input.ExpectedRevision {
		conflict(w, r, v22_collector_repo.ToDTO(collector))
		return
	}
	if row.Status != "succeeded" || row.Format != "json" {
		fail(w, r, 409, "CAPTURE_UNAVAILABLE", "需要成功的 JSON 快照", false, "json-check", "")
		return
	}
	var definition struct {
		Steps []map[string]json.RawMessage `json:"steps"`
	}
	if json.Unmarshal([]byte(collector.Definition), &definition) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "草稿步骤无效", false, "json-check", "")
		return
	}
	var plan capture.JSONPlan
	found := false
	seen := map[string]bool{}
	for _, step := range definition.Steps {
		var id, kind string
		_ = json.Unmarshal(step["step_id"], &id)
		_ = json.Unmarshal(step["type"], &kind)
		if id == "" || seen[id] {
			fail(w, r, 400, "INVALID_ARGUMENT", "步骤 ID 缺失或重复", false, "json-check", "")
			return
		}
		seen[id] = true
		if id == input.StepID && kind == "json_records" {
			plan, err = capture.DecodePlan(step["config"])
			if err != nil {
				fail(w, r, 400, "INVALID_ARGUMENT", "JSON 规则无效", false, "json-check", "")
				return
			}
			found = true
		}
	}
	if !found {
		fail(w, r, 400, "INVALID_ARGUMENT", "JSON 记录步骤不存在", false, "json-check", "")
		return
	}
	result, err := capture.InspectJSON(row.Content, plan)
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "json-check", "")
		return
	}
	result["capture_id"] = row.Id
	result["content_hash"] = row.ContentHash
	result["revision"] = collector.Revision
	ok(w, r, result)
}
