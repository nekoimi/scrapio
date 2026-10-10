package v3

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/nekoimi/scrapio/internal/repo/v22_overview_repo"
)

func overviewRead(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (any, error)) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, err := fn(ctx, admin.Id)
	if err != nil {
		switch {
		case errors.Is(err, v22_overview_repo.ErrNotFound):
			fail(w, r, 404, "NOT_FOUND", "方案或分页锚点不存在，请重新读取", false, "overview", "")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			fail(w, r, 408, "OVERVIEW_TIMEOUT", "概览读取超时，请重试", true, "overview", "")
		default:
			fail(w, r, 500, "INTERNAL", "概览读取失败，不能据此判断数据为空或方案健康", true, "storage", "")
		}
		return
	}
	ok(w, r, value)
}

func Home(w http.ResponseWriter, r *http.Request) {
	overviewRead(w, r, func(ctx context.Context, owner int64) (any, error) { return v22_overview_repo.Home(ctx, owner) })
}
func CollectorHealth(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案 ID 无效", false, "overview", "")
		return
	}
	overviewRead(w, r, func(ctx context.Context, owner int64) (any, error) { return v22_overview_repo.Health(ctx, owner, id) })
}
func CollectorHealthList(w http.ResponseWriter, r *http.Request) {
	limit, err := readLimit(r)
	filter := r.URL.Query().Get("attention")
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && err == nil {
		_, err = v22_overview_repo.PositiveID(cursor)
	}
	if err != nil || filter != "" && filter != "true" && filter != "false" {
		fail(w, r, 400, "INVALID_ARGUMENT", "健康列表分页或筛选无效", false, "overview", "")
		return
	}
	overviewRead(w, r, func(ctx context.Context, owner int64) (any, error) {
		return v22_overview_repo.List(ctx, owner, filter == "true", cursor, limit)
	})
}
