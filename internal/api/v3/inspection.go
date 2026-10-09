package v3

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_command_repo"
)

// All inspection operations authorize the owned session, read only, and require
// the displayed frame version. They never refresh a stale token behind the user.
func InspectBrowserPage(browser *drission_rod.DrissionRod, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		var input editor.Inspection
		if operation == "dom" {
			q := r.URL.Query()
			input.PageStateID, input.ElementID = q.Get("page_state_id"), q.Get("element_id")
			for key, dest := range map[string]*int{"offset": &input.Offset, "limit": &input.Limit} {
				if raw := q.Get(key); raw != "" {
					value, err := strconv.Atoi(raw)
					if err != nil {
						fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "inspect", key)
						return
					}
					*dest = value
				}
			}
		} else {
			r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
			if err := request.Parse(r, &input); err != nil {
				fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "inspect", "")
				return
			}
		}
		if err := input.Validate(operation); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "inspect", "")
			return
		}
		if operation == "record-preview" {
			collector, has, err := v22_collector_repo.Get(admin.Id, session.CollectorId)
			if err != nil {
				writeSessionError(w, r, err)
				return
			}
			if !has || collector.Status == "archived" {
				fail(w, r, 404, "NOT_FOUND", "方案不可用", false, "records", "")
				return
			}
			if collector.Revision != input.ExpectedRevision || collector.EntryURL != session.TargetURL {
				conflict(w, r, v22_collector_repo.ToDTO(collector))
				return
			}
			var definition struct {
				Steps []map[string]any `json:"steps"`
			}
			if err = json.Unmarshal([]byte(collector.Definition), &definition); err != nil {
				writeSessionError(w, r, err)
				return
			}
			input.RecordPlan = nil
			for _, step := range definition.Steps {
				if step["step_id"] == input.StepID {
					plan, parseErr := editor.RecordPlanFromStep(step)
					if parseErr != nil {
						fail(w, r, 400, "INVALID_ARGUMENT", parseErr.Error(), false, "records", "config")
						return
					}
					input.RecordPlan = &plan
					break
				}
			}
			if input.RecordPlan == nil || input.Stage == "detail" && input.RecordPlan.Detail == nil {
				fail(w, r, 400, "INVALID_ARGUMENT", "记录步骤或详情路径不存在", false, "records", "step_id")
				return
			}
		}
		if session.Status != "ready" || !session.ExpiresAt.After(time.Now()) {
			fail(w, r, 410, "SESSION_GONE", "会话不可用，请恢复或重新打开", false, "inspect", "")
			return
		}
		if session.PageStateId != input.PageStateID {
			fail(w, r, 409, "STALE_PAGE_STATE", "画面已变化，请刷新后重新选择", false, "inspect", "page_state_id")
			return
		}
		rows, err := v22_command_repo.List(admin.Id, session.Id)
		if err != nil {
			writeSessionError(w, r, err)
			return
		}
		for _, row := range rows {
			if row.Status == "queued" || row.Status == "running" || row.Status == "uncertain" {
				fail(w, r, 409, "COMMAND_PENDING", "请先确认上一动作回执", false, "inspect", "")
				return
			}
		}
		if browser == nil {
			fail(w, r, 503, "BROWSER_UNAVAILABLE", "浏览器服务未配置", true, "inspect", "")
			return
		}
		result, err := browser.InspectEditorPage(r.Context(), session.Id, w.Header().Get("X-Request-ID"), operation, input)
		if err != nil {
			code := browserCode(err)
			status := 503
			switch code {
			case "STALE_PAGE_STATE", "INSPECTION_BUSY":
				status = 409
			case "INVALID_ARGUMENT", "INSPECTION_FAILED":
				status = 400
			case "SESSION_GONE":
				status = 410
			}
			fail(w, r, status, code, err.Error(), status == 503, "inspect", "")
			return
		}
		ok(w, r, result)
	}
}
