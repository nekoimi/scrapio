package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/regression"
	"github.com/nekoimi/scrapio/internal/repo/v22_regression_repo"
)

func regressionDTO(j *table.V22Regression) map[string]any {
	return map[string]any{
		"job_id": j.Id, "collector_id": strconv.FormatInt(j.CollectorId, 10), "kind": j.Kind, "collector_revision": j.CollectorRevision, "base_version_id": j.BaseVersionId, "target_version_id": j.TargetVersionId, "base_hash": j.BaseHash, "target_hash": j.TargetHash, "snapshot_hash": j.SnapshotHash, "status": j.Status, "total": j.Total, "completed": j.Completed, "report": func() json.RawMessage {
			if j.Report == "" {
				return json.RawMessage(`{}`)
			}
			return json.RawMessage(j.Report)
		}(), "error_code": j.ErrorCode, "created_at": j.CreatedAt, "finished_at": j.FinishedAt, "network_accessed": false, "dry_run": true,
	}
}
func regressionRequest(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (any, error), create bool) {
	a, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	v, err := fn(ctx, a.Id)
	if err != nil {
		status, code, message := 500, "INTERNAL", "存储异常，请用原请求键查询结果"
		switch {
		case errors.Is(err, v22_regression_repo.ErrNotFound):
			status, code, message = 404, "NOT_FOUND", "方案、版本或回归不存在"
		case errors.Is(err, v22_regression_repo.ErrConflict):
			status, code, message = 409, "REGRESSION_CONFLICT", "草稿已变化、请求键冲突或任务仍在运行，请刷新"
		case errors.Is(err, v22_regression_repo.ErrCapacity):
			status, code, message = 409, "REGRESSION_CAPACITY", "上限：2个活动任务、100个保留任务或16MiB样例批；请删除已结束任务或缩小样例"
		case errors.Is(err, v22_regression_repo.ErrRestoreCapacity):
			status, code, message = 409, "RESTORE_CAPACITY", "已达到100个历史复制关联，请复用已经创建的新草稿；删除回归报告不会释放此容量"
		case errors.Is(err, v22_regression_repo.ErrInvalid):
			status, code, message = 400, "INVALID_ARGUMENT", "需要有效revision、版本选择及请求键"
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			status, code, message = 408, "REGRESSION_TIMEOUT", "响应未确认，请用原请求键查询，勿重复创建"
		}
		fail(w, r, status, code, message, status == 500 || status == 408, "regression", "")
		return
	}
	if create {
		accepted(w, r, v)
	} else {
		ok(w, r, v)
	}
}
func validVersion(id string) bool {
	if id == "" {
		return true
	}
	_, err := uuid.Parse(id)
	return err == nil
}
func CreateRegression(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, valid := collectorID(r)
		var i regression.Input
		key := r.Header.Get("Idempotency-Key")
		if !valid || parseSampleBody(w, r, &i) != nil || !validVersion(i.BaseVersionID) || !validVersion(i.TargetVersionID) {
			fail(w, r, 400, "INVALID_ARGUMENT", "方案/版本或请求无效", false, "regression", "")
			return
		}
		regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
			j, e := v22_regression_repo.Create(ctx, owner, id, key, kind, i)
			if e != nil {
				return nil, e
			}
			w.Header().Set("Location", "/api/v3/regressions/"+j.Id)
			return regressionDTO(j), nil
		}, true)
	}
}
func GetRegression(w http.ResponseWriter, r *http.Request) {
	id, key := mux.Vars(r)["job_id"], r.Header.Get("Idempotency-Key")
	if id != "" && !validVersion(id) || id == "" && (key == "" || len([]rune(key)) > 128) {
		fail(w, r, 400, "INVALID_ARGUMENT", "任务或请求键无效", false, "regression", "")
		return
	}
	regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		j, e := v22_regression_repo.Get(ctx, owner, id, key)
		if e != nil {
			return nil, e
		}
		return regressionDTO(j), nil
	}, false)
}
func ListRegressions(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, 400, "INVALID_ARGUMENT", "方案无效", false, "regression", "")
		return
	}
	regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		rows, e := v22_regression_repo.List(ctx, owner, id)
		if e != nil {
			return nil, e
		}
		items := []any{}
		for _, j := range rows {
			items = append(items, regressionDTO(&j))
		}
		return map[string]any{"items": items}, nil
	}, false)
}
func MutateRegression(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["job_id"]
	if !validVersion(id) || id == "" {
		fail(w, r, 400, "INVALID_ARGUMENT", "任务无效", false, "regression", "")
		return
	}
	regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		if r.Method == "DELETE" {
			return map[string]bool{"deleted": true}, v22_regression_repo.Delete(ctx, owner, id)
		}
		j, e := v22_regression_repo.Cancel(ctx, owner, id)
		if e != nil {
			return nil, e
		}
		return regressionDTO(j), nil
	}, false)
}
func restoreDTO(r *table.V22VersionRestore) any {
	return map[string]any{"restore_id": r.Id, "collector_id": strconv.FormatInt(r.CollectorId, 10), "version_id": r.VersionId, "target_collector_id": strconv.FormatInt(r.TargetCollectorId, 10), "created_at": r.CreatedAt}
}
func RestoreVersion(w http.ResponseWriter, r *http.Request) {
	id, valid := collectorID(r)
	var i v22_regression_repo.RestoreInput
	key := r.Header.Get("Idempotency-Key")
	if !valid || parseSampleBody(w, r, &i) != nil || i.VersionID == "" || !validVersion(i.VersionID) {
		fail(w, r, 400, "INVALID_ARGUMENT", "恢复请求无效", false, "regression", "")
		return
	}
	regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		v, e := v22_regression_repo.Restore(ctx, owner, id, key, i)
		if e != nil {
			return nil, e
		}
		return restoreDTO(v), nil
	}, false)
}
func GetVersionRestore(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len([]rune(key)) > 128 {
		fail(w, r, 400, "INVALID_ARGUMENT", "需要原请求键", false, "regression", "")
		return
	}
	regressionRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		v, e := v22_regression_repo.GetRestore(ctx, owner, key)
		if e != nil {
			return nil, e
		}
		return restoreDTO(v), nil
	}, false)
}
