package v3

import (
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_capture_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_command_repo"
	"net/http"
	"strconv"
	"time"
)

func CaptureBrowserPage(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, session, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		var requestBody struct {
			ExpectedRevision int    `json:"expected_revision"`
			PageStateID      string `json:"page_state_id"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if request.Parse(r, &requestBody) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "快照请求无效", false, "capture", "")
			return
		}
		input := capture.Input{CollectorID: strconv.FormatInt(session.CollectorId, 10), ExpectedRevision: requestBody.ExpectedRevision, Source: "browser", Format: "html", SessionID: session.Id, PageStateID: requestBody.PageStateID}
		key := r.Header.Get("Idempotency-Key")
		if err := input.Validate(); err != nil || key == "" || len([]rune(key)) > 128 {
			fail(w, r, 400, "INVALID_ARGUMENT", "需要有效的 revision、page_state_id 和请求键", false, "capture", "")
			return
		}
		collector, has, err := v22_collector_repo.Get(admin.Id, session.CollectorId)
		if err != nil {
			captureError(w, r, err)
			return
		}
		if !has {
			captureError(w, r, v22_capture_repo.ErrNotFound)
			return
		}
		row, dispatch, err := v22_capture_repo.Create(admin.Id, collector, key, input, capture.HTTPRequest{})
		if err != nil {
			captureError(w, r, err)
			return
		}
		if dispatch {
			result := capture.Result{Status: "failed", ErrorStage: "snapshot"}
			switch {
			case collector.EntryURL != session.TargetURL:
				result.ErrorCode = "SESSION_ENTRY_CHANGED"
			case session.Status != "ready" || !session.ExpiresAt.After(time.Now()):
				result.ErrorCode = "SESSION_GONE"
			case session.PageStateId != input.PageStateID:
				result.ErrorCode = "STALE_PAGE_STATE"
			case browser == nil:
				result.ErrorCode = "BROWSER_UNAVAILABLE"
			default:
				commands, listErr := v22_command_repo.List(admin.Id, session.Id)
				if listErr != nil {
					captureError(w, r, listErr)
					return
				}
				pending := false
				for _, command := range commands {
					if command.Status == "queued" || command.Status == "running" || command.Status == "uncertain" {
						pending = true
						break
					}
				}
				if pending {
					result.ErrorCode = "COMMAND_PENDING"
				} else {
					raw, rpcErr := browser.InspectEditorPage(r.Context(), session.Id, w.Header().Get("X-Request-ID"), "snapshot", editor.Inspection{PageStateID: input.PageStateID})
					if rpcErr != nil {
						result.ErrorCode = browserCode(rpcErr)
						if result.ErrorCode == "" {
							result.ErrorCode = "SNAPSHOT_FAILED"
						}
					} else {
						var document struct {
							HTML    string `json:"html"`
							URL     string `json:"current_url"`
							BaseURL string `json:"base_url"`
						}
						if json.Unmarshal(raw, &document) != nil || len(document.HTML) == 0 || len(document.HTML) > capture.MaxBytes {
							result.ErrorCode = "INVALID_RESPONSE"
						} else if _, err = capture.ValidateURL(document.URL); err != nil {
							result.ErrorCode = "INVALID_URL"
						} else if _, err = capture.ValidateURL(document.BaseURL); err != nil {
							result.ErrorCode = "INVALID_BASE_URL"
						} else {
							content, sanitizeErr := capture.Sanitize(document.HTML, "html", "")
							if sanitizeErr != nil {
								result.ErrorCode = "INVALID_CONTENT"
							} else {
								result = capture.Result{Status: "succeeded", Content: content, Hash: capture.Hash([]byte(content)), Bytes: len(document.HTML), FinalURL: document.URL, BaseURL: document.BaseURL, ContentType: "text/html"}
							}
						}
					}
				}
			}
			if err = v22_capture_repo.Complete(row, result); err != nil {
				captureError(w, r, err)
				return
			}
		}
		row, err = v22_capture_repo.Get(admin.Id, row.Id)
		if err != nil {
			captureError(w, r, err)
			return
		}
		created(w, r, captureDTO(row))
	}
}
