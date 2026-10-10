package v3

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/repair"
	"github.com/nekoimi/scrapio/internal/repo/v22_governance_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_repair_repo"
)

func repairRequest(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (*repair.Context, error), create bool) {
	admin, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	h, err := fn(ctx, admin.Id)
	if err != nil {
		switch {
		case errors.Is(err, v22_governance_repo.ErrCapacity):
			governanceError(w, r, err)
		case errors.Is(err, v22_repair_repo.ErrNotFound):
			fail(w, r, 404, "NOT_FOUND", "原运行、修复上下文或输入不属于当前方案/用户", false, "repair", "")
		case errors.Is(err, v22_repair_repo.ErrConflict):
			fail(w, r, 409, "REPAIR_CONFLICT", "草稿或请求已变化，请刷新修复上下文；当前草稿未被覆盖", false, "repair", "")
		case errors.Is(err, v22_repair_repo.ErrCapacity):
			fail(w, r, 409, "REPAIR_CAPACITY", "已达到100个修复上下文，请先复用已创建的修复入口", false, "repair", "")
		case errors.Is(err, v22_repair_repo.ErrInvalid):
			fail(w, r, 400, "INVALID_ARGUMENT", "只可修复 failed/partial 正式运行；需要确认模式、草稿 revision 和请求键", false, "repair", "")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			fail(w, r, 408, "REPAIR_TIMEOUT", "修复请求未确认，请查询原请求，不要再次创建", true, "repair", "")
		default:
			fail(w, r, 500, "INTERNAL", "修复上下文存储异常，请查询原请求后再处理", true, "storage", "")
		}
		return
	}
	if create {
		w.Header().Set("Location", "/api/v3/repair-drafts/"+h.ID)
		created(w, r, h)
	} else {
		ok(w, r, h)
	}
}

func PrepareRepair(w http.ResponseWriter, r *http.Request) {
	id, doc := mux.Vars(r)["run_id"], r.URL.Query().Get("document_id")
	if _, err := uuid.Parse(id); err != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "运行ID无效", false, "repair", "")
		return
	}
	if doc != "" {
		if _, err := uuid.Parse(doc); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "文档ID无效", false, "repair", "")
			return
		}
	}
	repairRequest(w, r, func(ctx context.Context, owner int64) (*repair.Context, error) {
		return v22_repair_repo.Prepare(ctx, owner, id, doc)
	}, false)
}
func CreateRepairDraft(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	var i repair.Input
	key := r.Header.Get("Idempotency-Key")
	if !valid || parseSampleBody(w, r, &i) != nil || i.Validate() != nil || key == "" || len([]rune(key)) > 128 {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要有效运行、确认模式、revision和Idempotency-Key", false, "repair", "")
		return
	}
	repairRequest(w, r, func(ctx context.Context, owner int64) (*repair.Context, error) {
		return v22_repair_repo.Create(ctx, owner, id, key, i)
	}, true)
}
func GetRepairDraft(w http.ResponseWriter, r *http.Request) {
	id, key := mux.Vars(r)["repair_id"], r.Header.Get("Idempotency-Key")
	if id != "" {
		if _, err := uuid.Parse(id); err != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "修复ID无效", false, "repair", "")
			return
		}
	} else if key == "" || len([]rune(key)) > 128 {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要原请求键", false, "repair", "")
		return
	}
	repairRequest(w, r, func(ctx context.Context, owner int64) (*repair.Context, error) {
		return v22_repair_repo.Get(ctx, owner, id, key)
	}, false)
}
