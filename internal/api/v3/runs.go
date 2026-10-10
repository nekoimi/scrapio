package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/v22_run_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_version_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
)

func runDTO(row *table.V22Run) map[string]any {
	return map[string]any{"run_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "version_id": row.VersionId, "version_number": row.VersionNumber, "collector_revision": row.CollectorRevision, "definition_hash": row.DefinitionHash, "status": row.Status, "attempt": row.Attempt, "cancel_requested": row.CancelRequested, "current_step_id": row.CurrentStep, "current_stage": row.CurrentStage, "event_seq": row.EventSeq, "input": json.RawMessage(row.Input), "summary": json.RawMessage(row.Summary), "trigger_source": row.TriggerSource, "created_at": row.CreatedAt, "started_at": row.StartedAt, "finished_at": row.FinishedAt, "dry_run": false, "contract_version": runmodel.ContractVersion, "publication_contract": row.PublicationContract, "interpreter_version": row.InterpreterVersion}
}
func runListDTO(row *table.V22Run) map[string]any {
	value := runDTO(row)
	var summary runmodel.Summary
	if runmodel.Decode(row.Summary, &summary) == nil {
		value["summary"] = map[string]any{"status": summary.Status, "stop_reason": summary.Reason, "pages": summary.Pages, "candidates": summary.Candidates, "failed_step_id": summary.FailedStep, "failed_stage": summary.FailedStage, "committed": summary.Committed, "counts": summary.Counts, "warnings": summary.Warnings, "list_pages": summary.ListPages, "details": summary.Details, "duplicate_records": summary.DuplicateRecords, "duplicate_details": summary.DuplicateDetails, "last_checkpoint": summary.LastCheckpoint}
	}
	return value
}
func runError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, v22_run_repo.ErrCapacity), errors.Is(err, v22_run_repo.ErrOverlap):
		fail(w, r, 409, "RUN_CAPACITY", err.Error(), true, "run", "")
	case errors.Is(err, v22_run_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "run", "")
	case errors.Is(err, v22_run_repo.ErrNotFound), errors.Is(err, v22_version_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "运行、方案或发布版本不存在", false, "run", "")
	case errors.Is(err, v22_run_repo.ErrConflict):
		fail(w, r, 409, "RUN_CONFLICT", "请求键、版本能力或目标 Schema 已变化", false, "run", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		fail(w, r, 408, "RUN_TIMEOUT", "请求超时，请查询原请求结果", true, "run", "")
	default:
		fail(w, r, 500, "INTERNAL", "运行存储异常，请查询原请求结果", true, "storage", "")
	}
}
func CreateRun(browser *drission_rod.DrissionRod, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, auth := owner(r)
		if !auth {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		id, valid := collectorID(r)
		if !valid {
			fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "run", "")
			return
		}
		var input runmodel.Input
		if parseSampleBody(w, r, &input) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "运行范围及预算无效", false, "run", "")
			return
		}
		if err := input.Validate(); err != nil {
			runError(w, r, fmt.Errorf("%w: %s", v22_run_repo.ErrInvalid, err))
			return
		}
		version, err := v22_version_repo.Get(admin.Id, input.VersionID, "")
		if err != nil {
			runError(w, r, err)
			return
		}
		if version.CollectorId != id {
			runError(w, r, v22_run_repo.ErrNotFound)
			return
		}
		plan, _, err := v22_run_repo.VersionPlan(version)
		if err != nil {
			runError(w, r, err)
			return
		}
		caps := definitionCapabilities(r.Context(), admin.Id, version.EntryType, plan.URL, version.Definition, browser, cfg)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		row, err := v22_run_repo.Create(ctx, admin.Id, id, r.Header.Get("Idempotency-Key"), input, caps)
		if err != nil {
			runError(w, r, err)
			return
		}
		w.Header().Set("Location", "/api/v3/runs/"+row.Id)
		writeJSON(w, 202, map[string]any{"data": runDTO(row), "request_id": w.Header().Get("X-Request-ID")})
	}
}
func ListRuns(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	limit := 25
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	collector := int64(0)
	if raw := r.URL.Query().Get("collector_id"); raw != "" && err == nil {
		collector, err = strconv.ParseInt(raw, 10, 64)
		if collector < 1 {
			err = errors.New("invalid collector")
		}
	}
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "run", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, more, err := v22_run_repo.List(ctx, admin.Id, collector, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		runError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range rows {
		items = append(items, runListDTO(&rows[i]))
	}
	next := ""
	if more {
		next = rows[len(rows)-1].Id
	}
	ok(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next})
}
func loadRun(w http.ResponseWriter, r *http.Request) (*table.V22Run, bool) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return nil, false
	}
	id := mux.Vars(r)["run_id"]
	key := ""
	if id == "" {
		key = r.Header.Get("Idempotency-Key")
		if key == "" {
			fail(w, r, 400, "INVALID_ARGUMENT", "原请求键必填", false, "run", "")
			return nil, false
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_run_repo.Get(ctx, admin.Id, id, key)
	if err != nil {
		runError(w, r, err)
		return nil, false
	}
	return row, true
}
func GetRun(w http.ResponseWriter, r *http.Request) {
	row, has := loadRun(w, r)
	if has {
		ok(w, r, runDTO(row))
	}
}
func CancelRun(w http.ResponseWriter, r *http.Request) {
	row, has := loadRun(w, r)
	if !has {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_run_repo.Cancel(ctx, row.OwnerId, row.Id)
	if err != nil {
		runError(w, r, err)
		return
	}
	ok(w, r, runDTO(row))
}
func RunResults(w http.ResponseWriter, r *http.Request) {
	row, has := loadRun(w, r)
	if !has {
		return
	}
	after := int64(0)
	limit := 5
	var err error
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" && err == nil {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || after < 0 || limit < 1 || limit > 10 {
		fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "run", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := v22_run_repo.Results(ctx, row.Id, after, limit+1)
	if err != nil {
		runError(w, r, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := []json.RawMessage{}
	next := ""
	for _, doc := range rows {
		items = append(items, json.RawMessage(doc.Result))
		next = strconv.FormatInt(doc.Sequence, 10)
	}
	if !more {
		next = ""
	}
	ok(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next, "summary": json.RawMessage(row.Summary), "status": row.Status})
}
func RunEvents(w http.ResponseWriter, r *http.Request) {
	row, has := loadRun(w, r)
	if !has {
		return
	}
	after := int64(0)
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	if raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 || n > row.EventSeq {
			fail(w, r, 400, "INVALID_ARGUMENT", "事件序号无效，请重读运行状态", false, "run", "")
			return
		}
		after = n
	}
	flusher, valid := w.(http.Flusher)
	if !valid {
		fail(w, r, 500, "INTERNAL", "事件流不可用，请轮询", true, "run", "")
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
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		events, err := v22_run_repo.Events(ctx, row.Id, after)
		cancel()
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
		ctx, cancel = context.WithTimeout(r.Context(), 5*time.Second)
		current, err := v22_run_repo.Get(ctx, row.OwnerId, row.Id, "")
		cancel()
		if err != nil {
			return
		}
		if runmodel.Terminal(current.Status) && after >= current.EventSeq {
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
