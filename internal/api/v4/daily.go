package v4

import (
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/co"
	"github.com/nekoimi/scrapio/internal/collector"
	"net/http"
	"strconv"
)

func (a API) governance(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	v, e := co.Governance(r.Context(), o)
	respond(w, r, v, e)
}
func (a API) sourcePreview(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.SourceRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.SourcePreview(r.Context(), o, r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
func (a API) sourceDetail(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	offset := 0
	var e error
	if r.URL.Query().Get("offset") != "" {
		offset, e = strconv.Atoi(r.URL.Query().Get("offset"))
		if e != nil {
			fail(w, r, collector.Invalid("offset", "行偏移需为非负整数"))
			return
		}
	}
	_, limit := page(r)
	v, e := co.SourceDetail(r.Context(), o, mux.Vars(r)["source_id"], offset, limit)
	respond(w, r, v, e)
}
func (a API) schedule(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["task_id"]
	if r.Method == "GET" {
		v, e := co.TaskSchedule(r.Context(), o, id)
		respond(w, r, v, e)
		return
	}
	var p co.ScheduleRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.SaveSchedule(r.Context(), o, id, r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
func (a API) upgradePreview(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.UpgradeRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.UpgradePreview(r.Context(), o, mux.Vars(r)["task_id"], p)
	respond(w, r, v, e)
}
func (a API) archiveTask(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p struct {
		Revision int `json:"expected_revision"`
	}
	if !decode(w, r, &p) {
		return
	}
	v, e := co.ArchiveTask(r.Context(), o, mux.Vars(r)["task_id"], r.Header.Get("Idempotency-Key"), p.Revision)
	done(w, r, v, e, false)
}
func (a API) replay(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.ReplayRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.ReplayStage(r.Context(), o, mux.Vars(r)["run_id"], r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
func (a API) exportTable(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.ExportRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.ExportTable(r.Context(), o, mux.Vars(r)["table_id"], p)
	respond(w, r, v, e)
}
func (a API) upgradeSchema(w http.ResponseWriter, r *http.Request) {
	o, ok := owner(w, r)
	if !ok {
		return
	}
	var p co.SchemaUpgradeRequest
	if !decode(w, r, &p) {
		return
	}
	v, e := co.SchemaUpgrade(r.Context(), o, mux.Vars(r)["table_id"], r.Header.Get("Idempotency-Key"), p)
	done(w, r, v, e, false)
}
