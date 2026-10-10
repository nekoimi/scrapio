package v3

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_trial_repo"
	"github.com/nekoimi/scrapio/internal/trial"
)

func trialDTO(row *table.V22Trial) map[string]any {
	return map[string]any{
		"trial_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "collector_revision": row.CollectorRevision, "definition_hash": row.DefinitionHash,
		"status": row.Status, "cancel_requested": row.CancelRequested, "current_step_id": row.CurrentStep, "current_stage": row.CurrentStage, "event_seq": row.EventSeq,
		"input": json.RawMessage(row.Input), "summary": json.RawMessage(row.Summary), "created_at": row.CreatedAt, "started_at": row.StartedAt, "finished_at": row.FinishedAt,
		"dry_run": true, "formal_records_written": false,
	}
}
func trialError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_trial_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "trial", "")
	case errors.Is(err, v22_trial_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "试采、方案或输入不存在", false, "trial", "")
	case errors.Is(err, v22_trial_repo.ErrConflict):
		fail(w, r, 409, "TRIAL_CONFLICT", "请求键、Schema 或试采状态已变化", false, "trial", "")
	default:
		fail(w, r, 500, "INTERNAL", "试采存储异常，请查询原请求状态", true, "storage", "")
	}
}
func CreateTrial(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "trial", "")
		return
	}
	var input trial.Input
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "试采配置无效", false, "trial", "")
		return
	}
	row, err := v22_trial_repo.Create(admin.Id, id, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		trialError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v3/trials/"+row.Id)
	writeJSON(w, http.StatusAccepted, map[string]any{"data": trialDTO(row), "request_id": w.Header().Get("X-Request-ID")})
}
func ListTrials(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "trial", "")
		return
	}
	rows, err := v22_trial_repo.List(admin.Id, id)
	if err != nil {
		trialError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range rows {
		items = append(items, trialDTO(&rows[i]))
	}
	ok(w, r, map[string]any{"items": items, "limit": 20})
}
func loadTrial(w http.ResponseWriter, r *http.Request) (*table.V22Trial, bool) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return nil, false
	}
	key := ""
	id := mux.Vars(r)["trial_id"]
	if id == "" {
		key = r.Header.Get("Idempotency-Key")
		if key == "" || len([]rune(key)) > 128 {
			fail(w, r, 400, "INVALID_ARGUMENT", "原请求键无效", false, "trial", "")
			return nil, false
		}
	}
	row, err := v22_trial_repo.Get(admin.Id, id, key)
	if err != nil {
		trialError(w, r, err)
		return nil, false
	}
	return row, true
}
func GetTrial(w http.ResponseWriter, r *http.Request) {
	row, has := loadTrial(w, r)
	if has {
		ok(w, r, trialDTO(row))
	}
}
func CancelTrial(w http.ResponseWriter, r *http.Request) {
	row, has := loadTrial(w, r)
	if !has {
		return
	}
	row, err := v22_trial_repo.Cancel(row.OwnerId, row.Id)
	if err != nil {
		trialError(w, r, err)
		return
	}
	ok(w, r, trialDTO(row))
}
func DeleteTrial(w http.ResponseWriter, r *http.Request) {
	row, has := loadTrial(w, r)
	if !has {
		return
	}
	if err := v22_trial_repo.Delete(row.OwnerId, row.Id); err != nil {
		trialError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func TrialResults(w http.ResponseWriter, r *http.Request) {
	row, has := loadTrial(w, r)
	if !has {
		return
	}
	after := int64(0)
	limit := 5
	var err error
	if value := r.URL.Query().Get("cursor"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || after < 0 || limit < 1 || limit > 10 {
		fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "trial", "")
		return
	}
	rows, err := v22_trial_repo.Results(row.Id, after, limit+1)
	if err != nil {
		trialError(w, r, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := []json.RawMessage{}
	next := ""
	for _, document := range rows {
		items = append(items, json.RawMessage(document.Result))
		next = strconv.FormatInt(document.Sequence, 10)
	}
	if !more {
		next = ""
	}
	ok(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next, "summary": json.RawMessage(row.Summary), "status": row.Status})
}
func TrialEvents(w http.ResponseWriter, r *http.Request) {
	row, has := loadTrial(w, r)
	if !has {
		return
	}
	after := int64(0)
	value := r.Header.Get("Last-Event-ID")
	if value == "" {
		value = r.URL.Query().Get("after")
	}
	if value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 || n > row.EventSeq {
			fail(w, r, 400, "INVALID_ARGUMENT", "事件序号无效，请重新读取试采状态", false, "trial", "")
			return
		}
		after = n
	}
	flusher, valid := w.(http.Flusher)
	if !valid {
		fail(w, r, 500, "INTERNAL", "事件流不可用，请轮询状态", true, "trial", "")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	for {
		events, err := v22_trial_repo.Events(row.Id, after)
		if err != nil {
			return
		}
		for _, event := range events {
			if _, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Sequence, event.Payload); err != nil {
				return
			}
			after = event.Sequence
		}
		flusher.Flush()
		current, err := v22_trial_repo.Get(row.OwnerId, row.Id, "")
		if err != nil {
			return
		}
		if v22_trial_repo.Terminal(current.Status) && after >= current.EventSeq {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
