package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/v22_schedule_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/schedule"
)

func scheduleDTO(row *table.V22Schedule) any {
	if row == nil {
		return nil
	}
	return map[string]any{"collector_id": strconv.FormatInt(row.CollectorId, 10), "revision": row.Revision, "enabled": row.Enabled, "cron": row.Cron, "timezone": row.Timezone, "overlap": row.Overlap, "input": json.RawMessage(row.Input), "next_at": row.NextAt, "last_decision": row.LastDecision, "last_run_id": row.LastRunId, "updated_at": row.UpdatedAt}
}
func keyDTO(row table.V22APIKey) any {
	return map[string]any{"id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "name": row.Name, "input": json.RawMessage(row.Input), "created_at": row.CreatedAt, "revoked_at": row.RevokedAt}
}
func scheduleOwner(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return 0, 0, false
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "schedule", "")
		return 0, 0, false
	}
	return admin.Id, id, true
}
func GetSchedule(w http.ResponseWriter, r *http.Request) {
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_schedule_repo.Get(ctx, owner, id)
	if err != nil {
		runError(w, r, err)
		return
	}
	ok(w, r, scheduleDTO(row))
}
func SaveSchedule(w http.ResponseWriter, r *http.Request) {
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	var input schedule.Config
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "计划格式无效", false, "schedule", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_schedule_repo.Save(ctx, owner, id, input)
	if err != nil {
		runError(w, r, err)
		return
	}
	ok(w, r, scheduleDTO(row))
}
func ScheduleEvents(w http.ResponseWriter, r *http.Request) {
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := v22_schedule_repo.Events(ctx, owner, id)
	if err != nil {
		runError(w, r, err)
		return
	}
	if rows == nil {
		rows = []map[string]string{}
	}
	ok(w, r, map[string]any{"items": rows, "limit": 100})
}
func ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := v22_schedule_repo.Keys(ctx, owner, id)
	if err != nil {
		runError(w, r, err)
		return
	}
	items := []any{}
	for _, row := range rows {
		items = append(items, keyDTO(row))
	}
	ok(w, r, map[string]any{"items": items})
}
func CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	var input struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input runmodel.Input `json:"input"`
	}
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "凭据格式无效", false, "schedule", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, secret, err := v22_schedule_repo.CreateKey(ctx, owner, id, input.ID, input.Name, input.Input)
	if err != nil {
		runError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"credential": keyDTO(*row), "token": secret}, "request_id": w.Header().Get("X-Request-ID")})
}
func RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	owner, id, valid := scheduleOwner(w, r)
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := v22_schedule_repo.Revoke(ctx, owner, id, mux.Vars(r)["key_id"]); err != nil {
		runError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"revoked": true})
}

// This route is registered before the JWT subrouter; its credential grants this action only.
func APIRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, err := v22_schedule_repo.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		fail(w, r, 401, "UNAUTHENTICATED", "API 凭据无效", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "run", "")
		return
	}
	var input struct {
		Budget json.RawMessage `json:"budget,omitempty"`
	}
	if parseSampleBody(w, r, &input) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "仅允许 budget 覆盖", false, "run", "")
		return
	}
	budget, err := schedule.DecodeBudget(input.Budget)
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "run", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	row, err := v22_schedule_repo.Trigger(ctx, id, token, r.Header.Get("Idempotency-Key"), budget)
	if errors.Is(err, v22_schedule_repo.ErrAuth) {
		fail(w, r, 401, "UNAUTHENTICATED", "API 凭据无效、已撤销或不属于此方案", false, "auth", "")
		return
	}
	if err != nil {
		runError(w, r, err)
		return
	}
	// Do not expose frozen definitions, headers, output or general owner access to a trigger-only credential.
	writeJSON(w, 202, map[string]any{"data": map[string]any{"run_id": row.Id, "collector_id": strconv.FormatInt(id, 10), "version_id": row.VersionId, "status": row.Status, "trigger_source": row.TriggerSource}, "request_id": w.Header().Get("X-Request-ID")})
}
