package v3

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
)

// Me keeps the v2.2 identity contract independent from legacy menu roles.
func Me(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ok(w, r, map[string]any{"id": strconv.FormatInt(admin.Id, 10), "username": admin.Username})
}

// Capabilities describes only capabilities available in the new product domain.
// The existing one-shot browser Execute RPC does not provide an editor session.
func Capabilities(w http.ResponseWriter, r *http.Request) {
	ok(w, r, map[string]any{
		"editor_protocol":          "unavailable",
		"browser_ready":            false,
		"supported_actions":        []string{},
		"supports_live_inspection": false,
		"definition_versions":      []int{},
		"reason":                   "交互浏览器协议与新采集方案尚未接入",
	})
}

func CapabilitiesWithBrowser(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ready := false
		if browser != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			ready = browser.ProbeEditor(ctx) == nil
			cancel()
		}
		actions := []string{}
		inspection := false
		records := false
		if ready {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			if browser.ProbeEditorCommands(ctx) == nil {
				actions = editor.Actions
			}
			cancel()
			ctx, cancel = context.WithTimeout(r.Context(), 2*time.Second)
			inspection = browser.ProbeEditorInspection(ctx) == nil
			cancel()
			if inspection {
				ctx, cancel = context.WithTimeout(r.Context(), 2*time.Second)
				records = browser.ProbeEditorRecords(ctx) == nil
				cancel()
			}
		}
		reason := "浏览器服务未连接"
		if ready {
			reason = "editor.v1 已连接；动作能力见 supported_actions，页面创建需浏览器可用"
		}
		ok(w, r, map[string]any{
			"editor_protocol": "editor.v1", "browser_ready": false, "editor_service_connected": ready,
			"supported_actions": actions, "supports_live_inspection": inspection,
			"supports_record_preview": records,
			"supports_http_capture":   true, "supports_offline_capture": true,
			"supports_json_capture_check": true,
			"definition_versions":         []int{1}, "reason": reason,
		})
	}
}

// Home exposes an explicit pending state until the v2.2 data domain exists.
// Returning zero counts here would misleadingly imply an empty new database.
func Home(w http.ResponseWriter, r *http.Request) {
	ok(w, r, map[string]any{
		"status": "pending",
		"reason": "新采集方案和数据域尚未接入",
	})
}
