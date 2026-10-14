package v4

import (
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/co"
	"github.com/nekoimi/scrapio/internal/collector"
	"net/http"
	"strconv"
)

func (a API) mountTasks(v *mux.Router) {
	v.HandleFunc("/governance", a.governance).Methods("GET")
	v.HandleFunc("/sources/preview", a.sourcePreview).Methods("POST")
	v.HandleFunc("/sources/{source_id}", a.sourceDetail).Methods("GET")
	v.HandleFunc("/tasks/{task_id}/schedule", a.schedule).Methods("GET", "POST")
	v.HandleFunc("/tasks/{task_id}/upgrade-preview", a.upgradePreview).Methods("POST")
	v.HandleFunc("/tasks/{task_id}/archive", a.archiveTask).Methods("POST")
	v.HandleFunc("/runs/{run_id}/replay", a.replay).Methods("POST")
	v.HandleFunc("/tables/{table_id}/export", a.exportTable).Methods("POST")
	v.HandleFunc("/tables/{table_id}/schema", a.upgradeSchema).Methods("POST")
	v.HandleFunc("/collector-versions/{version_id}", a.collectorVersion).Methods("GET")
	v.HandleFunc("/tables/{table_id}", a.tableView).Methods("GET")
	v.HandleFunc("/tasks", a.tasks).Methods("GET", "POST")
	v.HandleFunc("/tasks/input-preview", a.inputPreview).Methods("POST")
	v.HandleFunc("/tasks/save-preview", a.savePreview).Methods("POST")
	v.HandleFunc("/tasks/mapping-preview", a.mappingPreview).Methods("POST")
	v.HandleFunc("/tasks/{task_id}", a.task).Methods("GET")
	v.HandleFunc("/tasks/{task_id}/draft", a.task).Methods("GET", "PUT")
	v.HandleFunc("/tasks/{task_id}/versions", a.taskVersions).Methods("GET", "POST")
	v.HandleFunc("/tasks/{task_id}/runs", a.startTask).Methods("POST")
	v.HandleFunc("/tables", a.tables).Methods("GET", "POST")
	v.HandleFunc("/tables/{table_id}/records", a.tableRecords).Methods("GET")
	v.HandleFunc("/records/{record_id}", a.recordHistory).Methods("GET")
	v.HandleFunc("/runs", a.runs).Methods("GET")
	v.HandleFunc("/runs/{run_id}", a.run).Methods("GET")
	v.HandleFunc("/runs/{run_id}/control", a.controlRun).Methods("POST")
	v.HandleFunc("/runs/{run_id}/retry-failed", a.retryFailed).Methods("POST")
	v.HandleFunc("/runs/{run_id}/events", a.runEvents).Methods("GET")
	v.HandleFunc("/runs/{run_id}/work-items/{item_id}", a.workItem).Methods("GET")
	v.HandleFunc("/runs/{run_id}/work-items/{item_id}/resolve", a.resolveItem).Methods("POST")
	v.HandleFunc("/runs/{run_id}/work-items/{item_id}/attempts/{attempt_id}/commands/{command_id}", a.itemReceipt).Methods("GET")
	v.HandleFunc("/runs/{run_id}/{kind:work-items|outputs|observations|dispatches|gather-groups}", a.runPage).Methods("GET")
}
func (a API) mappingPreview(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		InputMode string         `json:"input_mode,omitempty"`
		Up        string         `json:"upstream_version_id"`
		Down      string         `json:"downstream_version_id"`
		Edge      co.TaskEdge    `json:"edge"`
		Input     map[string]any `json:"input"`
		Output    map[string]any `json:"output"`
	}
	if !decode(w, r, &p) {
		return
	}
	v, e := co.TaskMappingPreview(r.Context(), owner, p.Up, p.Down, p.Edge, p.Input, p.Output, p.InputMode)
	respond(w, r, v, e)
}
func (a API) collectorVersion(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.CollectorVersion(r.Context(), owner, mux.Vars(r)["version_id"])
	respond(w, r, v, e)
}
func (a API) tableView(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.TableView(r.Context(), owner, mux.Vars(r)["table_id"])
	respond(w, r, v, e)
}
func (a API) tasks(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		value, e := co.TaskList(r.Context(), owner, cursor, limit)
		respond(w, r, value, e)
		return
	}
	var p struct {
		Name       string            `json:"name"`
		Definition co.TaskDefinition `json:"definition"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.CreateTask(r.Context(), owner, p.Name, r.Header.Get("Idempotency-Key"), p.Definition)
	done(w, r, o, e, false)
}
func respond(w http.ResponseWriter, r *http.Request, value any, e error) {
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, value)
}
func (a API) task(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["task_id"]
	if r.Method == "GET" {
		t, e := co.GetTask(r.Context(), owner, id)
		if e != nil {
			fail(w, r, e)
			return
		}
		write(w, r, 200, t.View())
		return
	}
	var p struct {
		Revision   int               `json:"expected_revision"`
		Definition co.TaskDefinition `json:"definition"`
	}
	if !decode(w, r, &p) {
		return
	}
	t, e := co.SaveTask(r.Context(), owner, id, p.Revision, p.Definition)
	if e != nil {
		fail(w, r, e)
		return
	}
	write(w, r, 200, t.View())
}
func (a API) taskVersions(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["task_id"]
	if r.Method == "GET" {
		value, e := co.TaskVersions(r.Context(), owner, id)
		respond(w, r, value, e)
		return
	}
	var p struct {
		Revision int `json:"expected_revision"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.PublishTask(r.Context(), owner, id, r.Header.Get("Idempotency-Key"), p.Revision)
	done(w, r, o, e, false)
}
func (a API) inputPreview(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Version string           `json:"collector_version_id"`
		Inputs  []map[string]any `json:"inputs"`
	}
	if !decode(w, r, &p) {
		return
	}
	v, e := co.TaskInputPreview(r.Context(), owner, p.Version, p.Inputs)
	respond(w, r, v, e)
}
func (a API) savePreview(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Version    string                `json:"collector_version_id"`
		Save       co.SaveBinding        `json:"save"`
		Candidates []collector.Candidate `json:"candidates"`
		Port       string                `json:"port"`
	}
	if !decode(w, r, &p) {
		return
	}
	v, e := co.PreviewSave(r.Context(), owner, p.Version, p.Save, p.Candidates, p.Port)
	respond(w, r, v, e)
}
func (a API) startTask(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Version   string           `json:"task_version_id"`
		Namespace string           `json:"namespace"`
		Trial     *co.TrialOptions `json:"trial,omitempty"`
	}
	if !decode(w, r, &p) {
		return
	}
	var options []co.TrialOptions
	if p.Trial != nil {
		options = append(options, *p.Trial)
	}
	o, e := co.StartTask(r.Context(), owner, mux.Vars(r)["task_id"], r.Header.Get("Idempotency-Key"), p.Version, p.Namespace, options...)
	done(w, r, o, e, false)
}
func (a API) tables(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		cursor, limit := page(r)
		v, e := co.Tables(r.Context(), owner, cursor, limit)
		respond(w, r, v, e)
		return
	}
	var p struct {
		Name   string         `json:"name"`
		Schema co.TableSchema `json:"schema"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.CreateDataTable(r.Context(), owner, p.Name, r.Header.Get("Idempotency-Key"), p.Schema)
	done(w, r, o, e, false)
}
func (a API) runs(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	cursor, limit := page(r)
	v, e := co.Runs(r.Context(), owner, cursor, limit)
	respond(w, r, v, e)
}
func (a API) run(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.RunDetail(r.Context(), owner, mux.Vars(r)["run_id"], r.URL.Query().Get("summary") == "1")
	respond(w, r, v, e)
}
func (a API) controlRun(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Action string `json:"action"`
		Epoch  *int64 `json:"expected_control_epoch"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.Epoch == nil || *p.Epoch < 0 {
		fail(w, r, collector.Invalid("expected_control_epoch", "请刷新运行状态后提交控制代次"))
		return
	}
	o, e := co.ControlRun(r.Context(), owner, mux.Vars(r)["run_id"], r.Header.Get("Idempotency-Key"), p.Action, *p.Epoch)
	done(w, r, o, e, false)
}
func (a API) retryFailed(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &p) {
		return
	}
	o, e := co.RetryFailed(r.Context(), owner, mux.Vars(r)["run_id"], r.Header.Get("Idempotency-Key"), p.Confirmed)
	done(w, r, o, e, false)
}
func (a API) runEvents(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		var e error
		after, e = strconv.ParseInt(raw, 10, 64)
		if e != nil {
			fail(w, r, collector.Invalid("after", "事件游标应为整数"))
			return
		}
	}
	v, e := co.RunEvents(r.Context(), owner, mux.Vars(r)["run_id"], after)
	respond(w, r, v, e)
}
func (a API) runPage(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	cursor, limit := page(r)
	vars := mux.Vars(r)
	v, e := co.RunPage(r.Context(), owner, vars["run_id"], vars["kind"], cursor, limit)
	respond(w, r, v, e)
}
func (a API) workItem(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	v, e := co.ItemDetail(r.Context(), owner, vars["run_id"], vars["item_id"], r.URL.Query().Get("source_cursor"))
	respond(w, r, v, e)
}
func (a API) resolveItem(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Action    string `json:"action"`
		Confirmed bool   `json:"confirmed"`
	}
	if !decode(w, r, &p) {
		return
	}
	vars := mux.Vars(r)
	o, e := co.ResolveItem(r.Context(), owner, vars["run_id"], vars["item_id"], r.Header.Get("Idempotency-Key"), p.Action, p.Confirmed)
	done(w, r, o, e, false)
}
func (a API) itemReceipt(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	attempt, e := co.OriginalCommand(r.Context(), owner, vars["run_id"], vars["item_id"], vars["attempt_id"], vars["command_id"])
	if e != nil {
		fail(w, r, e)
		return
	}
	if a.Browser == nil {
		fail(w, r, collector.Fault("BROWSER_UNAVAILABLE", "receipt", "浏览器未连接；回执缺失不代表动作未执行"))
		return
	}
	v, e := a.Browser.GetEditorCommand(r.Context(), attempt.SessionID, vars["command_id"])
	respond(w, r, v, asBrowser(e))
}
func (a API) tableRecords(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	cursor, limit := page(r)
	v, e := co.TableRecords(r.Context(), owner, mux.Vars(r)["table_id"], cursor, limit)
	respond(w, r, v, e)
}
func (a API) recordHistory(w http.ResponseWriter, r *http.Request) {
	owner, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.RecordHistory(r.Context(), owner, mux.Vars(r)["record_id"])
	respond(w, r, v, e)
}
