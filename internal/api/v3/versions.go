package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/credential"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_version_repo"
)

func publicationError(w http.ResponseWriter, r *http.Request, err error) {
	var revision *v22_collector_repo.RevisionConflict
	switch {
	case errors.As(err, &revision):
		conflict(w, r, v22_collector_repo.ToDTO(revision.Latest))
	case errors.Is(err, v22_version_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", err.Error(), false, "publish", "")
	case errors.Is(err, v22_version_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "方案、版本或发布证据不存在", false, "publish", "")
	case errors.Is(err, v22_version_repo.ErrConflict):
		fail(w, r, 409, "PUBLISH_STALE", "草稿、样例、试采、Schema、能力或请求键已变化，或检查已过期；请重新检查并与当前发布版本比较、确认差异", false, "publish", "")
	case errors.Is(err, v22_version_repo.ErrBlocked):
		fail(w, r, 409, "PUBLISH_BLOCKED", "发布证据存在阻断项，请重新检查并修正", false, "publish", "")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		fail(w, r, 408, "CHECK_TIMEOUT", "发布检查超时，请查询原请求", true, "publish", "")
	default:
		fail(w, r, 500, "INTERNAL", "发布存储异常，请查询原请求结果", true, "storage", "")
	}
}
func publicationCapabilities(ctx context.Context, ownerID, collectorID int64, browser *drission_rod.DrissionRod, cfg *config.Config) (publication.Capabilities, error) {
	row, has, err := v22_collector_repo.Get(ownerID, collectorID)
	if err != nil {
		return publication.Capabilities{}, err
	}
	if !has {
		return publication.Capabilities{}, v22_version_repo.ErrNotFound
	}
	return definitionCapabilities(ctx, ownerID, row.EntryType, row.EntryURL, row.Definition, browser, cfg), nil
}
func definitionCapabilities(ctx context.Context, ownerID int64, entryType, entryURL, definition string, browser *drission_rod.DrissionRod, cfg *config.Config) publication.Capabilities {
	c := publication.Capabilities{DefinitionHash: publication.DefinitionHash(definition), Contract: publication.ContractVersion, Interpreter: extraction.InterpreterVersion, Actions: []string{}}
	if entryType == "json" {
		c.HTTP = true
		var root map[string]json.RawMessage
		if json.Unmarshal([]byte(definition), &root) == nil {
			request, err := capture.DecodeRequest(root["http_request"])
			if err == nil {
				var httpCfg *config.HTTPEntryConfig
				if cfg != nil {
					httpCfg = cfg.HTTPEntry
				}
				_, _, _, err = capture.Credential(httpCfg, ownerID, entryURL, request.CredentialRef)
				c.CredentialReady = err == nil
				if credential.ID(request.CredentialRef) != "" && err == nil {
					row, _, e := v22_credential_repo.Resolve(ctx, ownerID, entryURL, request.CredentialRef, "http_header")
					if e != nil {
						c.CredentialReady = false
					} else {
						c.CredentialRevision = row.Revision
						c.CredentialUpdatedAt = row.UpdatedAt
					}
				}
			}
		}
		return c
	}
	ref, authErr := credential.DefinitionRef([]byte(definition))
	if authErr != nil {
		return c
	}
	if ref != "" {
		row, _, e := v22_credential_repo.Resolve(ctx, ownerID, entryURL, ref, "browser_cookie")
		if e != nil {
			return c
		}
		c.CredentialRevision = row.Revision
		c.CredentialUpdatedAt = row.UpdatedAt
		c.CredentialReady = browser != nil && browser.ProbeEditorAuthorization(ctx) == nil
	}
	c.BrowserProtocol = drission_rod.EditorProtocolVersion
	if browser == nil {
		return c
	}
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c.Session = browser.ProbeEditor(probe) == nil
	if c.Session {
		c.Commands = browser.ProbeEditorCommands(probe) == nil
		c.Snapshot = browser.ProbeEditorSnapshot(probe) == nil
	}
	if c.Commands {
		c.Actions = append([]string{}, editor.Actions...)
	}
	return c
}
func publishCheckDTO(row *table.V22PublishCheck) map[string]any {
	return map[string]any{
		"check_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "collector_revision": row.CollectorRevision, "trial_id": row.TrialId,
		"definition_hash": row.DefinitionHash, "manifest_hash": row.ManifestHash, "capability_hash": row.CapabilityHash, "accept_limited": row.AcceptLimited,
		"ready": row.Ready, "result": json.RawMessage(row.Result), "manifest": json.RawMessage(row.Manifest), "capabilities": json.RawMessage(row.Capabilities), "created_at": row.CreatedAt, "expires_at": row.ExpiresAt,
	}
}
func versionDTO(row *table.V22Version, detail bool) map[string]any {
	value := map[string]any{"version_id": row.Id, "collector_id": strconv.FormatInt(row.CollectorId, 10), "number": row.Number, "collector_revision": row.CollectorRevision, "name": row.Name, "entry_type": row.EntryType, "definition_hash": row.DefinitionHash, "check_id": row.CheckId, "trial_id": row.TrialId, "contract_version": row.ContractVersion, "interpreter_version": row.InterpreterVersion, "capability_hash": row.CapabilityHash, "manifest_hash": row.ManifestHash, "note": row.Note, "published_by": strconv.FormatInt(row.PublishedBy, 10), "created_at": row.CreatedAt, "run_available": true, "run_unavailable_reason": ""}
	if detail {
		value["definition"] = json.RawMessage(row.Definition)
		value["output_schema"] = json.RawMessage(row.OutputSchema)
		value["runtime_config"] = json.RawMessage(row.RuntimeConfig)
		if row.DifferenceReview != "" {
			value["difference_review"] = json.RawMessage(row.DifferenceReview)
		}
	}
	return value
}
func CreatePublishCheck(browser *drission_rod.DrissionRod, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, auth := owner(r)
		if !auth {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		id, valid := collectorID(r)
		if !valid {
			fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "publish", "")
			return
		}
		var input v22_version_repo.CheckInput
		if parseSampleBody(w, r, &input) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "发布检查请求无效", false, "publish", "")
			return
		}
		if err := input.Validate(); err != nil {
			publicationError(w, r, err)
			return
		}
		caps, err := publicationCapabilities(r.Context(), admin.Id, id, browser, cfg)
		if err != nil {
			publicationError(w, r, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		row, err := v22_version_repo.CreateCheck(ctx, admin.Id, id, r.Header.Get("Idempotency-Key"), input, caps)
		if err != nil {
			publicationError(w, r, err)
			return
		}
		created(w, r, publishCheckDTO(row))
	}
}
func GetPublishCheck(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "publish", "")
		return
	}
	checkID := mux.Vars(r)["check_id"]
	key := ""
	if checkID == "" {
		key = r.Header.Get("Idempotency-Key")
		if key == "" {
			fail(w, r, 400, "INVALID_ARGUMENT", "原请求键必填", false, "publish", "")
			return
		}
	}
	row, err := v22_version_repo.GetCheck(admin.Id, id, checkID, key)
	if err != nil {
		publicationError(w, r, err)
		return
	}
	ok(w, r, publishCheckDTO(row))
}
func PublishVersion(browser *drission_rod.DrissionRod, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, auth := owner(r)
		if !auth {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		id, valid := collectorID(r)
		if !valid {
			fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "publish", "")
			return
		}
		var input v22_version_repo.PublishInput
		if parseSampleBody(w, r, &input) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "发布请求无效", false, "publish", "")
			return
		}
		if err := input.Validate(); err != nil {
			publicationError(w, r, err)
			return
		}
		caps, err := publicationCapabilities(r.Context(), admin.Id, id, browser, cfg)
		if err != nil {
			publicationError(w, r, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		row, err := v22_version_repo.Publish(ctx, admin.Id, id, r.Header.Get("Idempotency-Key"), input, caps)
		if err != nil {
			publicationError(w, r, err)
			return
		}
		created(w, r, versionDTO(row, true))
	}
}
func ListVersions(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "publish", "")
		return
	}
	limit, before := 25, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			before, err = strconv.Atoi(raw)
		}
	}
	if err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "分页参数无效", false, "publish", "")
		return
	}
	rows, more, err := v22_version_repo.List(admin.Id, id, before, limit)
	if err != nil {
		publicationError(w, r, err)
		return
	}
	items := []map[string]any{}
	for i := range rows {
		items = append(items, versionDTO(&rows[i], false))
	}
	next := ""
	if more {
		next = strconv.Itoa(rows[len(rows)-1].Number)
	}
	ok(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next})
}
func GetVersion(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id := mux.Vars(r)["version_id"]
	key := ""
	if id == "" {
		key = r.Header.Get("Idempotency-Key")
		if key == "" {
			fail(w, r, 400, "INVALID_ARGUMENT", "原请求键必填", false, "publish", "")
			return
		}
	}
	row, err := v22_version_repo.Get(admin.Id, id, key)
	if err != nil {
		publicationError(w, r, err)
		return
	}
	ok(w, r, versionDTO(row, true))
}
func VersionEvidence(w http.ResponseWriter, r *http.Request) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	row, err := v22_version_repo.Get(admin.Id, mux.Vars(r)["version_id"], "")
	if err != nil {
		publicationError(w, r, err)
		return
	}
	check, err := v22_version_repo.GetCheck(admin.Id, row.CollectorId, row.CheckId, "")
	if err != nil {
		publicationError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"version_id": row.Id, "check": publishCheckDTO(check), "samples": json.RawMessage(check.Evidence)})
}
