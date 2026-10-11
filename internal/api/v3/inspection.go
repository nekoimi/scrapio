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
	log "github.com/sirupsen/logrus"
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
			status, message := inspectionFailure(code)
			entry := log.WithFields(log.Fields{"request_id": w.Header().Get("X-Request-ID"), "operation": operation, "error_code": code})
			if status >= 500 {
				entry.Warn("editor inspection unavailable")
			} else {
				entry.Debug("editor inspection rejected")
			}
			fail(w, r, status, code, message, status == 503, "inspect", "")
			return
		}
		ok(w, r, result)
	}
}

// Keep recovery hints stable and never expose a remote exception or locator text.
// Older browser services returning INVALID_ARGUMENT remain compatible.
func inspectionFailure(code string) (int, string) {
	switch code {
	case "NO_ELEMENT":
		return 400, "此位置没有可选元素，请选择可见内容"
	case "OUTSIDE_RECORD_SCOPE":
		return 400, "请选择记录范围内部的字段，或调整记录范围"
	case "RECORD_SCOPE_EMPTY":
		return 400, "记录范围没有匹配元素，请重新选择记录范围"
	case "INVALID_LOCATOR":
		return 400, "CSS / XPath 语法无效，请修正规则；相对 XPath 需以点开头"
	case "ELEMENT_REFERENCE_STALE":
		return 409, "元素已失效，请重新点选或按当前规则验证"
	case "INSPECTION_LIMIT":
		return 400, "检查超过数量限制，请缩小规则范围或重新打开会话"
	case "STALE_FRAME":
		return 409, "截图依据已更新，请更新画面后重新点选；配置已保留"
	case "STALE_PAGE_STATE":
		return 409, "页面已变化，请更新画面后重新验证；当前配置可保留"
	case "INSPECTION_BUSY":
		return 409, "页面暂时忙碌，请等待当前操作结束后重试"
	case "SESSION_GONE":
		return 410, "会话已结束，请恢复或重新打开后重新验证"
	case "INVALID_ARGUMENT":
		return 400, "检查参数、元素或定位规则无效，请重新点选或核对规则"
	case "INSPECTION_FAILED":
		return 503, "页面检查失败，请更新画面或确认服务连接后重试"
	default:
		return 503, "检查服务暂时不可用，请确认连接后重试"
	}
}
