package v4

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/co"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"io"
	"net/http"
	"strconv"
	"time"
	"xorm.io/xorm"
)

type API struct {
	Browser *drission_rod.DrissionRod
	Config  *config.Config
}

func Mount(r *mux.Router, browser *drission_rod.DrissionRod, cfg *config.Config) {
	a := API{Browser: browser, Config: cfg}
	v := r.PathPrefix("/v4").Subrouter()
	a.mountTasks(v)
	v.HandleFunc("/capabilities", a.capabilities).Methods("GET")
	v.HandleFunc("/collectors", a.collectors).Methods("GET", "POST")
	v.HandleFunc("/collectors/{collector_id}", a.item).Methods("GET")
	v.HandleFunc("/collectors/{collector_id}/archive", a.archive).Methods("POST")
	v.HandleFunc("/collectors/{collector_id}/draft", a.draft).Methods("GET", "PUT")
	v.HandleFunc("/collectors/{collector_id}/tests", a.tests).Methods("GET", "POST")
	v.HandleFunc("/collectors/{collector_id}/samples", a.samples).Methods("GET", "POST")
	v.HandleFunc("/collectors/{collector_id}/versions", a.versions).Methods("GET", "POST")
	v.HandleFunc("/collectors/{collector_id}/publish-checks", a.check).Methods("POST")
	v.HandleFunc("/collectors/{collector_id}/editor-sessions", a.createSession).Methods("POST")
	v.HandleFunc("/operations/by-key", a.byKey).Methods("GET")
	v.HandleFunc("/operations/{operation_id}", a.operation).Methods("GET", "DELETE")
	v.HandleFunc("/operations/{operation_id}/receipt", a.testReceipt).Methods("GET")
	v.HandleFunc("/samples/{sample_id}", a.deleteSample).Methods("DELETE")
	v.HandleFunc("/sessions/{session_id}", a.session).Methods("GET", "DELETE")
	v.HandleFunc("/sessions/{session_id}/inspections", a.inspect).Methods("POST")
	v.HandleFunc("/sessions/{session_id}/commands", a.command).Methods("POST")
	v.HandleFunc("/sessions/{session_id}/commands/{command_id}", a.receipt).Methods("GET")
	v.HandleFunc("/sessions/{session_id}/frame", a.frame).Methods("GET")
}
func write(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "request_id": middleware.RequestID(r.Context())})
}
func fail(w http.ResponseWriter, r *http.Request, e error) {
	status := http.StatusBadRequest
	var typed *collector.Error
	if errors.Is(e, co.ErrNotFound) {
		status = http.StatusNotFound
		typed = &collector.Error{Code: "NOT_FOUND", Message: "资源不存在或不属于当前账号"}
	} else if errors.As(e, &typed) {
		if typed.Code == "CONFLICT" || typed.Code == "REVISION_CONFLICT" || typed.Code == "IDEMPOTENCY_CONFLICT" || typed.Code == "SCHEMA_CONFLICT" {
			status = http.StatusConflict
		}
		if typed.Code == "BROWSER_UNAVAILABLE" || typed.Code == "CAPABILITY_UNAVAILABLE" {
			status = http.StatusServiceUnavailable
		}
	} else {
		status = http.StatusInternalServerError
		typed = &collector.Error{Code: "INTERNAL_ERROR", Message: "操作暂时失败，请用原幂等键查询结果", Retryable: true}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": typed, "request_id": middleware.RequestID(r.Context())})
}
func owner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	sub, ok := r.Context().Value(request.ContextJwtUser).(string)
	id, e := strconv.ParseInt(sub, 10, 64)
	if !ok || e != nil || id <= 0 {
		w.WriteHeader(http.StatusUnauthorized)
		return 0, false
	}
	admin := new(table.Admin)
	if db.Instance() == nil {
		fail(w, r, collector.Fault("CAPABILITY_UNAVAILABLE", "database", "数据库尚未就绪"))
		return 0, false
	}
	dbSession := db.Instance().NewSession().Context(r.Context())
	defer dbSession.Close()
	found, e := dbSession.ID(id).Get(admin)
	if e != nil {
		fail(w, r, e)
		return 0, false
	}
	if !found {
		w.WriteHeader(http.StatusUnauthorized)
		return 0, false
	}
	return id, true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if e == nil && (len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{') {
		e = collector.Invalid("request", "请求必须是JSON对象")
	}
	if e == nil {
		e = collector.Strict(data, v)
	}
	if e != nil {
		fail(w, r, collector.Invalid("request", e.Error()))
		return false
	}
	return true
}
func page(r *http.Request) (string, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return r.URL.Query().Get("cursor"), limit
}
func id(r *http.Request) string { return mux.Vars(r)["collector_id"] }
func done(w http.ResponseWriter, r *http.Request, o *co.Operation, e error, async bool) {
	if e != nil {
		fail(w, r, e)
		return
	}
	if async {
		write(w, r, 202, o.View())
	} else {
		write(w, r, 200, json.RawMessage(o.Result))
	}
}
func (a API) capabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := owner(w, r); !ok {
		return
	}
	ready := false
	reason := "浏览器服务未配置或未提供独立context协议"
	if a.Browser != nil {
		ctx, cancel := contextTimeout(r, 2*time.Second)
		e := a.Browser.ProbeEditorIsolation(ctx)
		cancel()
		ready = e == nil
		if ready {
			reason = ""
		}
	}
	write(w, r, 200, map[string]any{"contract_version": "co.contract.v1", "definition_version": "collector.v1", "interpreter_version": collector.Interpreter, "publish_version": "co.publish.v1", "http_ready": true, "browser_ready": ready, "browser_protocol": "editor.isolated.v1", "reason": reason, "tasks_ready": true, "task_definition_version": "task.v1", "task_limits": map[string]any{"stages": 20, "source": []string{"manual", "table", "file"}, "inputs": 100, "snapshot_inputs": 1000, "file_formats": []string{"csv", "json"}, "schedules": "fixed_interval", "subscriptions_ready": true, "subscription_initial_modes": []string{"backfill", "future"}, "subscription_batch_inputs": 100, "subscription_events": []string{"created", "updated"}, "subscription_cursor": "transactional_table_sequence", "scoped_replay": true, "governance": true, "connections": true, "single_parent": false, "input_modes": []string{"single", "union", "join", "batch"}, "max_incoming_edges": 5, "max_edges": 60, "join_ready": true, "batch_aggregation_ready": true, "gather_window": "run_completion", "gather_max_rows": 1000, "gather_max_bytes": 4194304, "join_duplicates": "reject", "batch_aggregations": []string{"collect"}, "input_policies": []string{"keep_all", "merge_identical"}, "input_merge_scope": "run_stage", "max_dispatches": 10000, "same_table_stages": false, "output_ports": 5, "save_modes": []string{"table", "pass_through"}, "port_transaction": "page_atomic", "pass_through_emit": "valid"}, "jsonpath_subset": "$, .key, [index], [*]", "test_limits": map[string]int{"pages": 3, "records": 100, "duration_ms": 60000}})
}
func (a API) collectors(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		data, e := co.List(r.Context(), owner, cursor, limit)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, data)
		return
	}
	var p struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.Create(r.Context(), owner, p.Name, r.Header.Get("Idempotency-Key"))
	done(w, r, o, e, false)
}
func (a API) item(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	c, e := co.Get(r.Context(), owner, id(r))
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, c.View())
}
func (a API) draft(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.item(w, r)
		return
	}
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Revision   int             `json:"expected_revision"`
		Definition json.RawMessage `json:"definition"`
	}
	if !decode(w, r, &p) {
		return
	}
	d, e := collector.Decode(p.Definition)
	if e != nil {
		fail(w, r, e)
		return
	}
	c, e := co.Save(r.Context(), owner, id(r), p.Revision, d)
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, c.View())
}
func (a API) tests(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		data, e := co.Tests(r.Context(), owner, id(r), cursor, limit)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, data)
		return
	}
	var p struct {
		Revision int            `json:"expected_revision"`
		Input    map[string]any `json:"input"`
		Mode     string         `json:"mode"`
		SampleID string         `json:"sample_id,omitempty"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.Queue(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), "testCollector", co.TestPayload{Revision: p.Revision, Input: p.Input, Mode: p.Mode, SampleID: p.SampleID})
	done(w, r, o, e, true)
}
func (a API) check(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Revision   int    `json:"expected_revision"`
		EvidenceID string `json:"evidence_id"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.Queue(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), "checkPublication", co.TestPayload{Revision: p.Revision, EvidenceID: p.EvidenceID})
	done(w, r, o, e, true)
}
func (a API) versions(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		data, e := co.Versions(r.Context(), owner, id(r), cursor, limit)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, data)
		return
	}
	var p struct {
		Revision int    `json:"expected_revision"`
		CheckID  string `json:"check_id"`
	}
	if !decode(w, r, &p) {
		return
	}
	// Replays query the same immutable publication; new writes recheck live capability.
	_, lookupErr := co.ByKey(r.Context(), owner, "publishCollector", id(r), r.Header.Get("Idempotency-Key"))
	if errors.Is(lookupErr, co.ErrNotFound) {
		check, e := co.GetOperation(r.Context(), owner, p.CheckID)
		if e != nil {
			fail(w, r, e)
			return
		}
		var frozen co.TestPayload
		if check.ResourceID != id(r) || check.Operation != "checkPublication" || json.Unmarshal([]byte(check.Payload), &frozen) != nil {
			fail(w, r, co.ErrNotFound)
			return
		}
		evidence, e := co.GetOperation(r.Context(), owner, frozen.EvidenceID)
		if e != nil {
			fail(w, r, e)
			return
		}
		var tested co.TestPayload
		if json.Unmarshal([]byte(evidence.Payload), &tested) != nil {
			fail(w, r, collector.Invalid("check_id", "发布证据损坏"))
			return
		}
		ctx, cancel := contextTimeout(r, 4*time.Second)
		e = co.CheckAvailability(ctx, owner, frozen.Definition, tested.Input, a.Browser, a.Config)
		cancel()
		if e != nil {
			fail(w, r, e)
			return
		}
	} else if lookupErr != nil {
		fail(w, r, lookupErr)
		return
	}
	o, e := co.Publish(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), p.Revision, p.CheckID)
	done(w, r, o, e, false)
}
func (a API) samples(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		data, e := co.Samples(r.Context(), owner, id(r), cursor, limit)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, data)
		return
	}
	var p struct {
		Revision   int                  `json:"expected_revision"`
		EvidenceID string               `json:"evidence_id,omitempty"`
		Input      map[string]any       `json:"input,omitempty"`
		Snapshots  []collector.Document `json:"snapshots,omitempty"`
		Expected   []map[string]any     `json:"expected_outputs,omitempty"`
	}
	if !decode(w, r, &p) {
		return
	}
	var o *co.Operation
	var e error
	if p.EvidenceID != "" {
		if p.Snapshots != nil || p.Input != nil {
			fail(w, r, collector.Invalid("sample", "证据保存与手动导入不能同时使用"))
			return
		}
		o, e = co.SaveSample(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), p.Revision, p.EvidenceID, p.Expected)
	} else {
		o, e = co.ImportSample(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), p.Revision, p.Input, p.Snapshots, p.Expected)
	}
	done(w, r, o, e, false)
}
func (a API) archive(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Revision int `json:"expected_revision"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.Archive(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), p.Revision)
	done(w, r, o, e, false)
}
func (a API) operation(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	opID := mux.Vars(r)["operation_id"]
	if r.Method == "DELETE" {
		e := co.DeleteTest(r.Context(), owner, opID)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, map[string]bool{"deleted": true})
		return
	}
	o, e := co.GetOperation(r.Context(), owner, opID)
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, o.View())
}
func (a API) byKey(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	o, e := co.ByKey(r.Context(), owner, q.Get("operation"), q.Get("resource_id"), q.Get("key"))
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, o.View())
}
func (a API) deleteSample(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	e := co.DeleteSample(r.Context(), owner, mux.Vars(r)["sample_id"])
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, map[string]bool{"deleted": true})
}
func (a API) createSession(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Revision int            `json:"expected_revision"`
		Input    map[string]any `json:"input"`
		TTL      int            `json:"ttl_seconds"`
		Viewport struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"viewport"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.TTL < 30 || p.TTL > 600 || p.Viewport.Width < 320 || p.Viewport.Width > 2560 || p.Viewport.Height < 240 || p.Viewport.Height > 1600 {
		fail(w, r, collector.Invalid("viewport", "需要有效视口和30..600秒TTL"))
		return
	}
	o, e := co.Queue(r.Context(), owner, id(r), r.Header.Get("Idempotency-Key"), "createEditorSession", co.TestPayload{Revision: p.Revision, Input: p.Input, TTL: p.TTL, Width: p.Viewport.Width, Height: p.Viewport.Height})
	done(w, r, o, e, true)
}
func (a API) ownedSession(w http.ResponseWriter, r *http.Request) (*co.Session, bool) {
	owner, ok := owner(w, r)
	if !ok {
		return nil, false
	}
	s, e := co.GetSession(r.Context(), owner, mux.Vars(r)["session_id"])
	if e == nil && a.Browser == nil {
		e = collector.Fault("BROWSER_UNAVAILABLE", "session", "浏览器服务未配置")
	}
	if e != nil {
		fail(w, r, e)
		return nil, false
	}
	return s, true
}
func (a API) session(w http.ResponseWriter, r *http.Request) {
	s, ok := a.ownedSession(w, r)
	if !ok {
		return
	}
	if r.Method == "DELETE" {
		e := a.Browser.CloseEditorSession(r.Context(), s.ID)
		if e != nil {
			fail(w, r, asBrowser(e))
			return
		}
		write(w, r, 200, map[string]bool{"closed": true})
		return
	}
	state, e := a.Browser.GetEditorSession(r.Context(), s.ID)
	if e != nil {
		fail(w, r, asBrowser(e))
		return
	}
	write(w, r, 200, co.SessionView(s, state))
}
func asBrowser(e error) error {
	if b, ok := e.(*drission_rod.BrowserError); ok {
		return collector.Fault(b.Code, "browser", b.Message)
	}
	return e
}
func (a API) inspect(w http.ResponseWriter, r *http.Request) {
	s, ok := a.ownedSession(w, r)
	if !ok {
		return
	}
	var p struct {
		Operation   string          `json:"operation"`
		PageStateID string          `json:"page_state_id"`
		Input       json.RawMessage `json:"input"`
	}
	if !decode(w, r, &p) {
		return
	}
	var input editor.Inspection
	if collector.Strict(p.Input, &input) != nil {
		fail(w, r, collector.Invalid("input", "检查参数无效"))
		return
	}
	input.PageStateID = p.PageStateID
	if p.Operation == "snapshot" {
		if input.PageStateID == "" {
			fail(w, r, collector.Invalid("page_state_id", "需要页面状态"))
			return
		}
	} else if e := input.Validate(p.Operation); e != nil {
		fail(w, r, collector.Invalid("input", e.Error()))
		return
	}
	d, e := collector.Decode([]byte(s.Definition))
	if e == nil {
		var state drission_rod.EditorSessionState
		state, e = a.Browser.GetEditorSession(r.Context(), s.ID)
		if e == nil {
			e = collector.CheckOrigin(state.CurrentURL, d.Fetch.AllowedOrigins)
		}
	}
	if e != nil {
		fail(w, r, asBrowser(e))
		return
	}
	raw, e := a.Browser.InspectEditorPage(r.Context(), s.ID, r.Header.Get("Idempotency-Key"), p.Operation, input)
	if e != nil {
		fail(w, r, asBrowser(e))
		return
	}
	write(w, r, 200, raw)
}
func (a API) command(w http.ResponseWriter, r *http.Request) {
	s, ok := a.ownedSession(w, r)
	if !ok {
		return
	}
	var p struct {
		CommandID   string          `json:"command_id"`
		PageStateID string          `json:"page_state_id"`
		Action      string          `json:"action"`
		Confirmed   bool            `json:"confirmed"`
		Locator     *editor.Locator `json:"locator,omitempty"`
		Value       string          `json:"value,omitempty"`
		TimeoutMS   int             `json:"timeout_ms"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.CommandID == "" || len(p.CommandID) > 128 || p.CommandID != r.Header.Get("Idempotency-Key") {
		fail(w, r, collector.Invalid("command_id", "command_id必须与幂等键一致"))
		return
	}
	switch p.Action {
	case "navigate", "click", "input", "wait", "scroll":
	default:
		fail(w, r, collector.Invalid("action", "不支持此动作"))
		return
	}
	cmd := editor.Command{PageStateID: p.PageStateID, ExpectedRevision: s.Revision, Type: p.Action, Confirmed: p.Confirmed, Locator: p.Locator, Value: p.Value, TimeoutMS: p.TimeoutMS}
	e := cmd.Validate()
	if e == nil && p.Action == "navigate" {
		d, err := collector.Decode([]byte(s.Definition))
		e = err
		if e == nil {
			e = collector.CheckOrigin(p.Value, d.Fetch.AllowedOrigins)
		}
	}
	if e != nil {
		fail(w, r, collector.Invalid("command", e.Error()))
		return
	}
	payload := co.CommandPayload{CommandID: p.CommandID, Command: cmd}
	o, e := co.Mutate(r.Context(), s.OwnerID, "editorCommand", s.ID, p.CommandID, payload, true, func(db *xorm.Session, o *co.Operation) (any, error) {
		// One action per session, including unresolved outcomes across control-plane restarts.
		if _, e := db.QueryString("SELECT id FROM co_sessions WHERE id=? FOR UPDATE", s.ID); e != nil {
			return nil, e
		}
		count, e := db.Where("resource_id=? AND operation='editorCommand' AND (status IN ('queued','running') OR (status='failed' AND error->>'code'='OUTCOME_UNCERTAIN'))", s.ID).Count(new(co.Operation))
		if e != nil {
			return nil, e
		}
		if count > 0 {
			return nil, &collector.Error{Code: "CONFLICT", Message: "上一动作尚未结束或结果未知，请查询原回执；未确认前不执行新动作"}
		}
		return nil, nil
	})
	done(w, r, o, e, true)
}
func (a API) receipt(w http.ResponseWriter, r *http.Request) {
	s, ok := a.ownedSession(w, r)
	if !ok {
		return
	}
	receipt, e := a.Browser.GetEditorCommand(r.Context(), s.ID, mux.Vars(r)["command_id"])
	if e != nil {
		fail(w, r, asBrowser(e))
		return
	}
	write(w, r, 200, receipt)
}
func (a API) frame(w http.ResponseWriter, r *http.Request) {
	s, ok := a.ownedSession(w, r)
	if !ok {
		return
	}
	state, e := a.Browser.FrameEditorSession(r.Context(), s.ID, r.URL.Query().Get("page_state_id"))
	if e != nil {
		fail(w, r, asBrowser(e))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Page-State-Id", state.PageStateID)
	if state.State != nil {
		w.Header().Set("X-Scrapio-Frame", collector.JSON(state.State))
	}
	w.Header().Set("Access-Control-Expose-Headers", "X-Page-State-Id, X-Scrapio-Frame")
	_, _ = w.Write(state.Screenshot)
}

// Read the original test receipt after an uncertain result; this route never executes an action.
func (a API) testReceipt(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	o, e := co.GetOperation(r.Context(), owner, mux.Vars(r)["operation_id"])
	if e != nil {
		fail(w, r, e)
		return
	}
	var p struct {
		CommandID string `json:"command_id"`
	}
	if o.Operation != "testCollector" || json.Unmarshal([]byte(o.Payload), &p) != nil || p.CommandID == "" {
		fail(w, r, collector.Invalid("operation_id", "此测试没有网页动作回执"))
		return
	}
	if a.Browser == nil {
		fail(w, r, collector.Fault("BROWSER_UNAVAILABLE", "receipt", "浏览器未配置"))
		return
	}
	receipt, e := a.Browser.GetEditorCommand(r.Context(), o.ID, p.CommandID)
	if e != nil {
		fail(w, r, collector.Fault("OUTCOME_UNCERTAIN", "receipt", "原回执不可用；未重放。浏览器重启或会话过期后需人工确认"))
		return
	}
	write(w, r, 200, receipt)
}
