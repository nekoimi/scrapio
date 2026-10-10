package v3

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/repo/v22_quality_repo"
	"net/http"
	"strconv"
	"time"
)

func qualityRequest(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (any, error)) {
	a, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	v, e := fn(ctx, a.Id)
	if e != nil {
		status, code, msg := 500, "INTERNAL", "质量证据不可用，不能判断没有问题；请重新读取"
		switch {
		case errors.Is(e, v22_quality_repo.ErrNotFound):
			status, code, msg = 404, "NOT_FOUND", "方案、运行或问题不存在"
		case errors.Is(e, v22_quality_repo.ErrConflict):
			status, code, msg = 409, "QUALITY_CONFLICT", "策略/问题版本或恢复证据已变化，请刷新；本地修改保留"
		case errors.Is(e, v22_quality_repo.ErrInvalid):
			status, code, msg = 400, "INVALID_ARGUMENT", "阈值或基线无效；基线需本方案完整、已提交的实时正式运行"
		case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
			status, code, msg = 408, "QUALITY_TIMEOUT", "请求未确认，请重新读取策略或问题状态，不要自动再次提交"
		}
		fail(w, r, status, code, msg, status >= 500 || status == 408, "quality", "")
		return
	}
	ok(w, r, v)
}
func QualityPolicy(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案无效", false, "quality", "")
		return
	}
	var i v22_quality_repo.PolicyInput
	if r.Method == "PUT" && (parseSampleBody(w, r, &i) != nil || i.BaselineRunID != "" && !validVersion(i.BaselineRunID)) {
		fail(w, r, 400, "INVALID_ARGUMENT", "策略请求无效", false, "quality", "")
		return
	}
	qualityRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		if r.Method == "PUT" {
			return v22_quality_repo.SavePolicy(ctx, owner, id, i)
		}
		return v22_quality_repo.GetPolicy(ctx, owner, id)
	})
}
func CollectorQuality(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案无效", false, "quality", "")
		return
	}
	qualityRequest(w, r, func(ctx context.Context, owner int64) (any, error) { return v22_quality_repo.Read(ctx, owner, id) })
}
func QualityIssues(w http.ResponseWriter, r *http.Request) {
	status, cursor := r.URL.Query().Get("status"), r.URL.Query().Get("cursor")
	if status == "" {
		status = "active"
	}
	limit, e := readLimit(r)
	id := int64(0)
	if v := r.URL.Query().Get("collector_id"); v != "" {
		var parseErr error
		id, parseErr = strconv.ParseInt(v, 10, 64)
		if parseErr != nil || id < 1 {
			e = v22_quality_repo.ErrInvalid
		}
	}
	if e != nil || !validVersion(cursor) || (status != "active" && status != "all" && status != "open" && status != "ready" && status != "resolved") {
		fail(w, r, 400, "INVALID_ARGUMENT", "问题筛选/分页无效", false, "quality", "")
		return
	}
	qualityRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		return v22_quality_repo.ListIssues(ctx, owner, id, status, cursor, limit)
	})
}
func QualityIssue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["issue_id"]
	if _, e := uuid.Parse(id); e != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "问题ID无效", false, "quality", "")
		return
	}
	var i v22_quality_repo.ResolveInput
	if r.Method == "POST" && (parseSampleBody(w, r, &i) != nil || !validVersion(i.RecoveryRunID)) {
		fail(w, r, 400, "INVALID_ARGUMENT", "恢复确认无效", false, "quality", "")
		return
	}
	qualityRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		if r.Method == "POST" {
			return v22_quality_repo.Resolve(ctx, owner, id, i)
		}
		return v22_quality_repo.GetIssue(ctx, owner, id)
	})
}
