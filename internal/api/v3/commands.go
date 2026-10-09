package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_command_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_session_repo"
	log "github.com/sirupsen/logrus"
)

func commandDTO(row *table.V22EditorCommand) map[string]any {
	var input editor.Command
	_ = json.Unmarshal([]byte(row.Request), &input)
	var result drission_rod.EditorCommandResult
	_ = json.Unmarshal([]byte(row.Result), &result)
	return map[string]any{"command_id": row.Id, "session_id": row.SessionId, "status": row.Status, "type": input.Type, "request": input, "result": result, "created_at": row.CreatedAt, "deadline_at": row.DeadlineAt}
}

func commandError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_command_repo.ErrIdempotencyConflict):
		fail(w, r, 409, "IDEMPOTENCY_CONFLICT", err.Error(), false, "command", "")
	case errors.Is(err, v22_command_repo.ErrBusy):
		fail(w, r, 409, "COMMAND_PENDING", err.Error(), false, "command", "")
	case errors.Is(err, v22_command_repo.ErrNotFound) || errors.Is(err, v22_session_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", err.Error(), false, "command", "")
	case errors.Is(err, v22_session_repo.ErrSessionGone):
		writeSessionError(w, r, err)
	case err.Error() == "STALE_PAGE_STATE":
		fail(w, r, 409, "STALE_PAGE_STATE", "页面已变化，请更新画面", false, "command", "page_state_id")
	default:
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "command", "")
	}
}

func CreateBrowserCommand(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		var input editor.Command
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		if err := request.Parse(r, &input); err != nil {
			commandError(w, r, err)
			return
		}
		if err := input.Validate(); err != nil {
			commandError(w, r, err)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len([]rune(key)) > 128 {
			commandError(w, r, errors.New("Idempotency-Key is required and limited to 128 characters"))
			return
		}
		if browser == nil {
			fail(w, r, 503, "BROWSER_UNAVAILABLE", "浏览器服务未连接", true, "command", "")
			return
		}
		row, dispatch, err := v22_command_repo.Create(admin.Id, session.Id, key, input)
		if err != nil {
			commandError(w, r, err)
			return
		}
		if dispatch {
			go executeBrowserCommand(browser, row, input)
		}
		writeJSON(w, http.StatusAccepted, apiResponse{Data: commandDTO(row), RequestID: w.Header().Get("X-Request-ID")})
	}
}

func executeBrowserCommand(browser *drission_rod.DrissionRod, row *table.V22EditorCommand, input editor.Command) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	started, err := v22_command_repo.Start(row)
	if err != nil || !started {
		log.WithField("command_id", row.Id).WithError(err).Warn("editor command dispatch was not started")
		return
	}
	state, err := v22_session_repo.Get(row.OwnerId, row.SessionId)
	result := drission_rod.EditorCommandResult{Status: "failed", BeforePageStateID: input.PageStateID, ErrorCode: "SESSION_GONE", Error: "会话已结束，命令未执行"}
	if err == nil && state.Status == "ready" && state.ExpiresAt.After(time.Now()) {
		result, err = browser.ExecuteEditorCommand(ctx, row.SessionId, row.Id, input)
		if err != nil {
			result = drission_rod.EditorCommandResult{Status: "uncertain", BeforePageStateID: input.PageStateID, ErrorCode: "OUTCOME_UNCERTAIN", Error: "命令结果未确认，请查询回执；不会自动重发"}
			switch browserCode(err) {
			case "INVALID_ARGUMENT", "STALE_PAGE_STATE", "IDEMPOTENCY_CONFLICT", "SESSION_GONE":
				result.Status = "failed"
				result.ErrorCode = browserCode(err)
				result.Error = "命令被浏览器拒绝，未执行"
			}
		}
	}
	if err := v22_command_repo.Complete(row, input, result, true); err != nil {
		log.WithField("command_id", row.Id).WithError(err).Error("editor command result persistence failed; query receipt to reconcile")
	}
}

func GetBrowserCommand(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		id := mux.Vars(r)["command_id"]
		if _, err := uuid.Parse(id); err != nil {
			commandError(w, r, errors.New("invalid command_id"))
			return
		}
		row, err := v22_command_repo.Get(admin.Id, session.Id, id)
		if err != nil {
			commandError(w, r, err)
			return
		}
		// Reconcile only by querying. A queued row after a process crash is never
		// automatically redispatched because the website outcome may be unknown.
		if row.Status == "uncertain" || ((row.Status == "queued" || row.Status == "running") && !row.DeadlineAt.After(time.Now())) {
			var input editor.Command
			_ = json.Unmarshal([]byte(row.Request), &input)
			result := drission_rod.EditorCommandResult{Status: "uncertain", BeforePageStateID: input.PageStateID, ErrorCode: "OUTCOME_UNCERTAIN", Error: "尚无可确认的回执；检查页面或重新打开会话，不自动重试"}
			if browser != nil {
				if remote, err := browser.GetEditorCommand(r.Context(), session.Id, row.Id); err == nil && (remote.Status == "succeeded" || remote.Status == "failed" || remote.Status == "uncertain") {
					result = remote
				}
			}
			if err := v22_command_repo.Complete(row, input, result, false); err != nil {
				writeSessionError(w, r, err)
				return
			}
			row, err = v22_command_repo.Get(admin.Id, session.Id, id)
			if err != nil {
				writeSessionError(w, r, err)
				return
			}
		}
		ok(w, r, commandDTO(row))
	}
}

func ListBrowserCommands(w http.ResponseWriter, r *http.Request) {
	admin, session, loaded := loadOwnedSession(w, r)
	if !loaded {
		return
	}
	rows, err := v22_command_repo.List(admin.Id, session.Id)
	if err != nil {
		writeSessionError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		items = append(items, commandDTO(&rows[i]))
	}
	ok(w, r, map[string]any{"items": items, "limit": 50})
}

func checkpointDTO(row table.V22EditorCheckpoint) map[string]any {
	var snapshot map[string]any
	_ = json.Unmarshal([]byte(row.Snapshot), &snapshot)
	return map[string]any{"checkpoint_id": row.Id, "name": row.Name, "draft_revision": row.DraftRevision, "through_step_id": row.ThroughStepId, "snapshot": snapshot, "created_at": row.CreatedAt}
}

func CollectorCheckpoints(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		commandError(w, r, errors.New("invalid collector_id"))
		return
	}
	_, has, err := v22_collector_repo.Get(admin.Id, id)
	if err != nil {
		writeSessionError(w, r, err)
		return
	}
	if !has {
		commandError(w, r, v22_session_repo.ErrNotFound)
		return
	}
	if r.Method == http.MethodGet {
		rows, err := v22_command_repo.Checkpoints(admin.Id, id)
		if err != nil {
			writeSessionError(w, r, err)
			return
		}
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			items = append(items, checkpointDTO(row))
		}
		ok(w, r, map[string]any{"items": items})
		return
	}
	if r.Method == http.MethodDelete {
		if err := v22_command_repo.DeleteCheckpoint(admin.Id, id, mux.Vars(r)["checkpoint_id"]); err != nil {
			commandError(w, r, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	var input struct {
		Name             string `json:"name"`
		ExpectedRevision int    `json:"expected_revision"`
		ThroughStepID    string `json:"through_step_id"`
	}
	if err := request.Parse(r, &input); err != nil {
		commandError(w, r, err)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 160 || input.ExpectedRevision < 1 || input.ThroughStepID == "" {
		commandError(w, r, errors.New("name, expected_revision and through_step_id are required"))
		return
	}
	row, err := v22_command_repo.CreateCheckpoint(admin.Id, id, input.ExpectedRevision, input.Name, input.ThroughStepID)
	if err != nil {
		commandError(w, r, err)
		return
	}
	created(w, r, checkpointDTO(*row))
}
