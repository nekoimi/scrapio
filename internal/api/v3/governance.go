package v3

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/governance"
	"github.com/nekoimi/scrapio/internal/repo/v22_governance_repo"
	log "github.com/sirupsen/logrus"
)

func governanceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, v22_governance_repo.ErrInvalid):
		fail(w, r, 400, "INVALID_ARGUMENT", "请检查字段、版本和确认参数", false, "governance", "")
	case errors.Is(err, v22_governance_repo.ErrNotFound):
		fail(w, r, 404, "NOT_FOUND", "预览或数据表不存在", false, "governance", "")
	case errors.Is(err, v22_governance_repo.ErrCapacity):
		fail(w, r, 409, "ASSET_CAPACITY", "预览资产达到上限，请先清理无引用输入或调整容量", false, "capacity", "")
	case errors.Is(err, v22_governance_repo.ErrConflict):
		fail(w, r, 409, "GOVERNANCE_CONFLICT", "预览过期、存在阻断或依赖已变化，请重新预览；未执行修改", false, "governance", "")
	default:
		fail(w, r, 500, "INTERNAL", "操作结果未确认，请查询原预览或操作记录", false, "storage", "")
	}
}
func TableSchemaChanges(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	id, err := dataTableID(r)
	if err != nil {
		governanceError(w, r, v22_governance_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	switch {
	case r.Method == "GET":
		rows, err := v22_governance_repo.SchemaHistory(ctx, owner, id)
		if err != nil {
			governanceError(w, r, err)
			return
		}
		ok(w, r, map[string]any{"items": rows})
	case mux.Vars(r)["check_id"] != "":
		var input struct {
			Confirmed bool `json:"confirmed"`
		}
		if parseSampleBody(w, r, &input) != nil {
			governanceError(w, r, v22_governance_repo.ErrInvalid)
			return
		}
		result, err := v22_governance_repo.ApplySchema(ctx, owner, id, mux.Vars(r)["check_id"], input.Confirmed)
		if err != nil {
			governanceError(w, r, err)
			return
		}
		ok(w, r, result)
	default:
		var input v22_governance_repo.SchemaInput
		if parseSampleBody(w, r, &input) != nil {
			governanceError(w, r, v22_governance_repo.ErrInvalid)
			return
		}
		result, err := v22_governance_repo.CreateSchemaCheck(ctx, owner, id, input)
		if err != nil {
			governanceError(w, r, err)
			return
		}
		created(w, r, result)
	}
}
func RetentionSettings(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if r.Method == "PUT" {
		var input governance.Policy
		if parseSampleBody(w, r, &input) != nil {
			governanceError(w, r, v22_governance_repo.ErrInvalid)
			return
		}
		result, err := v22_governance_repo.SavePolicy(ctx, owner, input)
		if err != nil {
			governanceError(w, r, err)
			return
		}
		ok(w, r, result)
		return
	}
	result, err := v22_governance_repo.Settings(ctx, owner)
	if err != nil {
		governanceError(w, r, err)
		return
	}
	ok(w, r, result)
}
func RetentionCleanup(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if id := mux.Vars(r)["check_id"]; id != "" {
		var input struct {
			Confirmed bool `json:"confirmed"`
		}
		if parseSampleBody(w, r, &input) != nil {
			governanceError(w, r, v22_governance_repo.ErrInvalid)
			return
		}
		result, err := v22_governance_repo.ApplyCleanup(ctx, owner, id, input.Confirmed)
		if err != nil {
			governanceError(w, r, err)
			return
		}
		ok(w, r, result)
		return
	}
	result, err := v22_governance_repo.CreateCleanupCheck(ctx, owner)
	if err != nil {
		governanceError(w, r, err)
		return
	}
	created(w, r, result)
}
func OperationLogs(w http.ResponseWriter, r *http.Request) {
	owner, auth := dataOwner(w, r)
	if !auth {
		return
	}
	before, limit := int64(0), 25
	var err error
	if v := r.URL.Query().Get("cursor"); v != "" {
		before, err = strconv.ParseInt(v, 10, 64)
	}
	if err == nil {
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, err = strconv.Atoi(v)
		}
	}
	if err != nil {
		governanceError(w, r, v22_governance_repo.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, next, err := v22_governance_repo.Operations(ctx, owner, before, limit)
	if err != nil {
		governanceError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"items": rows, "next_cursor": next, "has_more": next != ""})
}

type operationWriter struct {
	http.ResponseWriter
	status int
}

func (w *operationWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *operationWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *operationWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Record the attempt BEFORE invoking a mutation. A failed audit insert rejects
// the mutation; a crash afterwards leaves a visible started/unknown operation.
// Outcomes describe HTTP requests, not a claim that all underlying writes share
// a transaction. Governance changes additionally write committed logs atomically.
func OperationAudit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" && r.Method != "PUT" && r.Method != "PATCH" && r.Method != "DELETE" {
			next.ServeHTTP(w, r)
			return
		}
		admin, auth := owner(r)
		if !auth {
			fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
			return
		}
		route := mux.CurrentRoute(r)
		template := ""
		if route != nil {
			template, _ = route.GetPathTemplate()
		}
		action := r.Method + " " + template
		resource := ""
		for _, key := range []string{"collector_id", "table_id", "credential_id", "run_id", "sample_id", "check_id", "version_id", "issue_id"} {
			if v := mux.Vars(r)[key]; v != "" {
				resource = v
				break
			}
		}
		// Path variables are identifiers only; redact arbitrary caller-controlled paths.
		if len(resource) > 64 {
			resource = ""
		}
		for _, c := range resource {
			if c != '-' && (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				resource = ""
				break
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		requestID := middleware.RequestID(r.Context())
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = "hash:" + capture.Hash([]byte(requestID))
		}
		id, err := v22_governance_repo.StartOperation(ctx, admin.Id, requestID, action, resource)
		cancel()
		if err != nil {
			fail(w, r, 503, "AUDIT_UNAVAILABLE", "操作记录暂不可写，变更尚未执行", true, "audit", "")
			return
		}
		recorder := &operationWriter{ResponseWriter: w}
		completed := false
		defer func() {
			status := recorder.status
			if status == 0 || !completed {
				status = 500
			}
			// Persist outcome even after the client has disconnected.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
			defer cancel()
			if err := v22_governance_repo.FinishOperation(ctx, admin.Id, id, status); err != nil {
				log.WithError(err).WithField("operation_id", id).Warn("v22 operation outcome unconfirmed")
			}
		}()
		next.ServeHTTP(recorder, r)
		completed = true
	})
}
