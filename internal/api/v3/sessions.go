package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_session_repo"
)

const editorSessionTTL = 2 * time.Minute

type createEditorSessionInput struct {
	CollectorID   string `json:"collector_id"`
	DraftRevision int    `json:"draft_revision"`
	Viewport      struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"viewport"`
}

type editorSessionDTO struct {
	SessionID                string         `json:"session_id"`
	CollectorID              string         `json:"collector_id"`
	DraftRevision            int            `json:"draft_revision"`
	Status                   string         `json:"status"`
	ExpiresAt                time.Time      `json:"expires_at"`
	PageStateID              string         `json:"page_state_id"`
	CurrentURL               string         `json:"current_url"`
	Viewport                 map[string]int `json:"viewport"`
	FrameURL                 string         `json:"frame_url"`
	HeartbeatIntervalSeconds int            `json:"heartbeat_interval_seconds"`
	Available                bool           `json:"available"`
	NeedsReopen              bool           `json:"needs_reopen"`
}

func sessionDTO(row *table.V22BrowserSession) editorSessionDTO {
	return editorSessionDTO{
		SessionID: row.Id, CollectorID: strconv.FormatInt(row.CollectorId, 10), DraftRevision: row.DraftRevision,
		Status: row.Status, ExpiresAt: row.ExpiresAt, PageStateID: row.PageStateId, CurrentURL: row.CurrentURL,
		Viewport: map[string]int{"width": row.ViewportWidth, "height": row.ViewportHeight},
		FrameURL: "/api/v3/browser-sessions/" + row.Id + "/frame", HeartbeatIntervalSeconds: 20,
		Available:   row.Status == "ready" && row.ExpiresAt.After(time.Now()),
		NeedsReopen: row.Status == "disconnected" || row.Status == "expired" || row.Status == "closed" || row.Status == "failed",
	}
}

func CreateBrowserSession(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, authenticated := owner(r)
		if !authenticated {
			fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		var input createEditorSessionInput
		if err := request.Parse(r, &input); err != nil {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "")
			return
		}
		collectorID, err := strconv.ParseInt(input.CollectorID, 10, 64)
		if err != nil || collectorID <= 0 || input.DraftRevision <= 0 {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "collector_id and draft_revision are required", false, "validate", "collector_id")
			return
		}
		if input.Viewport.Width == 0 {
			input.Viewport.Width = 1280
		}
		if input.Viewport.Height == 0 {
			input.Viewport.Height = 800
		}
		if input.Viewport.Width < 320 || input.Viewport.Width > 3840 || input.Viewport.Height < 240 || input.Viewport.Height > 2160 {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "viewport is outside supported limits", false, "validate", "viewport")
			return
		}
		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey == "" || len([]rune(idempotencyKey)) > 128 {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Idempotency-Key is required and limited to 128 characters", false, "validate", "Idempotency-Key")
			return
		}
		sessionID := uuid.NewString()
		row, err := v22_session_repo.Create(admin.Id, collectorID, input.DraftRevision, sessionID, idempotencyKey, input.Viewport.Width, input.Viewport.Height, editorSessionTTL)
		if err != nil {
			if errors.Is(err, v22_session_repo.ErrRevisionConflict) {
				latest, _, _ := v22_collector_repo.Get(admin.Id, collectorID)
				if latest != nil {
					conflict(w, r, v22_collector_repo.ToDTO(latest))
				} else {
					fail(w, r, http.StatusNotFound, "NOT_FOUND", "collector not found", false, "create_session", "collector_id")
				}
			} else if errors.Is(err, v22_session_repo.ErrIdempotencyConflict) {
				fail(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error(), false, "create_session", "Idempotency-Key")
			} else if errors.Is(err, v22_session_repo.ErrUnsupportedEntry) {
				fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "create_session", "collector_id")
			} else if errors.Is(err, v22_session_repo.ErrNotFound) {
				fail(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), false, "create_session", "collector_id")
			} else {
				fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
			}
			return
		}
		sessionID = row.Id
		if terminalSession(row.Status) || !row.ExpiresAt.After(time.Now()) {
			fail(w, r, http.StatusConflict, "IDEMPOTENCY_REPLAY_TERMINAL", "该幂等请求对应的会话已结束，请使用新的请求键", false, "create_session", "Idempotency-Key")
			return
		}
		if row.Status != "creating" {
			writeRefreshedSession(w, r, admin.Id, row.Id, browser)
			return
		}
		if browser == nil {
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", "浏览器服务未配置", true, "create_session", "")
			return
		}
		state, err := browser.CreateEditorSession(r.Context(), drission_rod.EditorSessionInput{SessionID: sessionID, URL: row.TargetURL, TTL: editorSessionTTL, Width: int32(row.ViewportWidth), Height: int32(row.ViewportHeight)})
		if err != nil {
			// A timeout does not prove that the remote creation failed. Keep
			// the request retryable with the same session ID; remote TTL reaps it.
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", err.Error(), true, "create_session", "")
			return
		}
		err = v22_session_repo.UpdateFromBrowser(admin.Id, sessionID, state.Status, state.PageStateID, state.CurrentURL, state.ExpiresAt)
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = browser.CloseEditorSession(cleanupCtx, sessionID)
			writeSessionError(w, r, err)
			return
		}
		row, err = v22_session_repo.Get(admin.Id, sessionID)
		if err != nil || terminalSession(row.Status) {
			if err == nil {
				err = v22_session_repo.ErrSessionGone
			}
			writeSessionError(w, r, err)
			return
		}
		created(w, r, sessionDTO(row))
	}
}

func GetBrowserSession(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if loaded {
			writeRefreshedSession(w, r, admin.Id, session.Id, browser)
		}
	}
}

func HeartbeatBrowserSession(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		if session.Status != "ready" || !session.ExpiresAt.After(time.Now()) {
			fail(w, r, http.StatusGone, "SESSION_GONE", "浏览器会话已结束或过期", false, "heartbeat", "session_id")
			return
		}
		if browser == nil {
			if err := v22_session_repo.SetStatus(admin.Id, session.Id, "disconnected"); err != nil {
				writeSessionError(w, r, err)
				return
			}
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", "浏览器服务未连接", true, "heartbeat", "")
			return
		}
		state, err := browser.HeartbeatEditorSession(r.Context(), session.Id, editorSessionTTL)
		if err != nil {
			status := sessionFailureStatus(err)
			if storageErr := v22_session_repo.SetStatus(admin.Id, session.Id, status); storageErr != nil {
				writeSessionError(w, r, storageErr)
				return
			}
			if status == "expired" {
				writeSessionError(w, r, v22_session_repo.ErrSessionGone)
			} else {
				fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", err.Error(), true, "heartbeat", "")
			}
			return
		}
		if err := v22_session_repo.UpdateFromBrowser(admin.Id, session.Id, state.Status, state.PageStateID, state.CurrentURL, state.ExpiresAt); err != nil {
			writeSessionError(w, r, err)
			return
		}
		session, err = v22_session_repo.Get(admin.Id, session.Id)
		if err != nil || terminalSession(session.Status) {
			if err == nil {
				err = v22_session_repo.ErrSessionGone
			}
			writeSessionError(w, r, err)
			return
		}
		ok(w, r, sessionDTO(session))
	}
}

func CloseBrowserSession(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		// Persist terminal state first so an in-flight heartbeat cannot reopen it.
		if err := v22_session_repo.SetStatus(admin.Id, session.Id, "closed"); err != nil {
			writeSessionError(w, r, err)
			return
		}
		if browser == nil {
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", "会话已停止；浏览器资源待释放，可重试关闭或等待 TTL 清理", true, "close_session", "")
			return
		}
		if err := browser.CloseEditorSession(r.Context(), session.Id); err != nil && sessionFailureStatus(err) != "expired" {
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", "会话已停止，浏览器资源释放失败，请重试关闭", true, "close_session", "")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ResumeBrowserSession(browser *drission_rod.DrissionRod) http.HandlerFunc {
	// Reconnect only to an existing tab; replay belongs to A04.
	return GetBrowserSession(browser)
}

func GetBrowserSessionFrame(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		if session.Status != "ready" || !session.ExpiresAt.After(time.Now()) {
			if session.Status == "ready" && !session.ExpiresAt.After(time.Now()) {
				if err := v22_session_repo.SetStatus(admin.Id, session.Id, "expired"); err != nil {
					writeSessionError(w, r, err)
					return
				}
			}
			fail(w, r, http.StatusGone, "SESSION_GONE", "浏览器会话已结束或过期", false, "frame", "session_id")
			return
		}
		if browser == nil {
			fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", "浏览器服务未连接", true, "frame", "")
			return
		}
		if r.URL.Query().Get("page_state_id") == "" {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "page_state_id is required", false, "frame", "page_state_id")
			return
		}
		state, err := browser.FrameEditorSession(r.Context(), session.Id, r.URL.Query().Get("page_state_id"))
		if err != nil {
			if browserCode(err) == "STALE_PAGE_STATE" {
				fail(w, r, http.StatusConflict, "STALE_PAGE_STATE", err.Error(), false, "frame", "page_state_id")
			} else if sessionFailureStatus(err) == "expired" {
				if storageErr := v22_session_repo.SetStatus(admin.Id, session.Id, "expired"); storageErr != nil {
					writeSessionError(w, r, storageErr)
					return
				}
				writeSessionError(w, r, v22_session_repo.ErrSessionGone)
			} else {
				fail(w, r, http.StatusServiceUnavailable, "BROWSER_UNAVAILABLE", err.Error(), true, "frame", "")
			}
			return
		}
		if err := v22_session_repo.UpdateFromBrowser(admin.Id, session.Id, state.Status, state.PageStateID, state.CurrentURL, state.ExpiresAt); err != nil {
			writeSessionError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Page-State-ID", state.PageStateID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(state.Screenshot)
	}
}

func BrowserSessionEvents(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		controller := http.NewResponseController(w)
		if !supportsFlush(w) {
			fail(w, r, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "当前 HTTP 服务不支持会话事件流", false, "events", "")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		lastPayload := ""
		eventID := 0
		send := func(row *table.V22BrowserSession) bool {
			payload, err := json.Marshal(sessionDTO(row))
			if err != nil {
				return false
			}
			if string(payload) == lastPayload {
				return true
			}
			lastPayload = string(payload)
			eventID++
			if _, err := fmt.Fprintf(w, "id: %d\nevent: session\ndata: %s\n\n", eventID, payload); err != nil {
				return false
			}
			return controller.Flush() == nil
		}

		if !send(session) || terminalSession(session.Status) {
			return
		}

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		keepalive := 0
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				keepalive++
				latest, err := refreshOwnedSessionFromBrowser(r, admin.Id, session.Id, browser)
				if err != nil {
					return
				}
				if !send(latest) || terminalSession(latest.Status) {
					return
				}
				if keepalive >= 5 {
					if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
						return
					}
					if controller.Flush() != nil {
						return
					}
					keepalive = 0
				}
			}
		}
	}
}

func supportsFlush(w http.ResponseWriter) bool {
	for {
		if _, ok := w.(http.Flusher); ok {
			return true
		}
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = wrapper.Unwrap()
	}
}

func refreshOwnedSessionFromBrowser(r *http.Request, ownerID int64, sessionID string, browser *drission_rod.DrissionRod) (*table.V22BrowserSession, error) {
	session, err := v22_session_repo.Get(ownerID, sessionID)
	if err != nil || terminalSession(session.Status) {
		return session, err
	}
	if !session.ExpiresAt.After(time.Now()) {
		err = v22_session_repo.SetStatus(ownerID, sessionID, "expired")
	} else if browser == nil {
		err = v22_session_repo.SetStatus(ownerID, sessionID, "disconnected")
	} else {
		state, browserErr := browser.GetEditorSession(r.Context(), sessionID)
		if browserErr != nil {
			err = v22_session_repo.SetStatus(ownerID, sessionID, sessionFailureStatus(browserErr))
		} else {
			err = v22_session_repo.UpdateFromBrowser(ownerID, sessionID, state.Status, state.PageStateID, state.CurrentURL, state.ExpiresAt)
		}
	}
	if err != nil && !errors.Is(err, v22_session_repo.ErrSessionGone) {
		return nil, err
	}
	// Always read persisted state: close/expiry may have won the race.
	return v22_session_repo.Get(ownerID, sessionID)
}

func sessionFailureStatus(err error) string {
	switch browserCode(err) {
	case "SESSION_EXPIRED", "SESSION_NOT_FOUND":
		return "expired"
	default:
		return "disconnected"
	}
}

func writeSessionError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, v22_session_repo.ErrSessionGone) {
		fail(w, r, http.StatusGone, "SESSION_GONE", "浏览器会话已结束或过期，请重新打开", false, "session", "session_id")
	} else {
		fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
	}
}

func writeRefreshedSession(w http.ResponseWriter, r *http.Request, ownerID int64, sessionID string, browser *drission_rod.DrissionRod) {
	session, err := refreshOwnedSessionFromBrowser(r, ownerID, sessionID, browser)
	if err == nil && terminalSession(session.Status) {
		err = v22_session_repo.ErrSessionGone
	}
	if err != nil {
		writeSessionError(w, r, err)
		return
	}
	ok(w, r, sessionDTO(session))
}

func terminalSession(status string) bool {
	return status == "closed" || status == "expired" || status == "failed"
}

func loadOwnedSession(w http.ResponseWriter, r *http.Request) (*table.Admin, *table.V22BrowserSession, bool) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return nil, nil, false
	}
	sessionID := mux.Vars(r)["session_id"]
	if _, err := uuid.Parse(sessionID); err != nil {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "session_id is invalid", false, "validate", "session_id")
		return nil, nil, false
	}
	session, err := v22_session_repo.Get(admin.Id, sessionID)
	if errors.Is(err, v22_session_repo.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), false, "lookup", "session_id")
		return nil, nil, false
	}
	if err != nil {
		fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
		return nil, nil, false
	}
	return admin, session, true
}

func browserCode(err error) string {
	var browserError *drission_rod.BrowserError
	if errors.As(err, &browserError) {
		return browserError.Code
	}
	return ""
}
