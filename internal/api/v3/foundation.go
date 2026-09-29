package v3

import "net/http"

// Me keeps the v2.2 identity contract independent from legacy menu roles.
func Me(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ok(w, r, map[string]any{"id": admin.Id, "username": admin.Username})
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

// Home exposes an explicit pending state until the v2.2 data domain exists.
// Returning zero counts here would misleadingly imply an empty new database.
func Home(w http.ResponseWriter, r *http.Request) {
	ok(w, r, map[string]any{
		"status": "pending",
		"reason": "新采集方案和数据域尚未接入",
	})
}
