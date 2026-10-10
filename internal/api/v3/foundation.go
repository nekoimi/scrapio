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
		snapshots := false
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
				ctx, cancel = context.WithTimeout(r.Context(), 2*time.Second)
				snapshots = browser.ProbeEditorSnapshot(ctx) == nil
				cancel()
			}
		}
		authorization := false
		if ready {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			authorization = browser.ProbeEditorAuthorization(ctx) == nil
			cancel()
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
			"supports_snapshot_capture":   snapshots, "supports_extraction_preview": true,
			"supports_samples": true, "supports_sample_checks": true,
			"supports_sample_regressions": true, "supports_version_comparisons": true, "supports_version_restores": true,
			"supports_credentials": true, "supports_browser_authorization": authorization,
			"supports_quality_policies": true, "supports_quality_issues": true,
			"supports_output_checks": true, "supports_logical_tables": true,
			"supports_trials": true, "supports_trial_events": true,
			"supports_publication": true, "supports_version_runs": true, "supports_continuous_runs": true, "supports_run_checkpoints": true, "supports_schedules": true, "supports_api_triggers": true, "supports_data_queries": true, "supports_data_views": true, "supports_data_exports": true, "supports_run_center": true, "supports_home_overview": true, "supports_collector_health": true, "supports_repair_drafts": true, "supports_run_retries": true, "run_retry_scopes": []string{"full_run"}, "run_contract": "run.v1",
			"definition_versions": []int{1}, "reason": reason,
		})
	}
}
