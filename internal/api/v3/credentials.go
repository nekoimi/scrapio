package v3

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/credential"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
	"io"
	"net/http"
	"strconv"
	"time"
)

func credentialBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return credential.ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return credential.ErrInvalid
	}
	return nil
}
func credentialRequest(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64) (any, error)) {
	a, auth := owner(r)
	if !auth {
		fail(w, r, 401, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	v, e := fn(ctx, a.Id)
	if e != nil {
		status, code, msg := 500, "CREDENTIAL_STORAGE_UNAVAILABLE", "凭据请求未确认，请读取服务器状态核对；不会自动重复保存"
		switch {
		case errors.Is(e, credential.ErrInvalid):
			status, code, msg = 400, "INVALID_ARGUMENT", "凭据配置无效；秘密限8KiB、有效期最多366天，环境引用仅允许 SCRAPIO_SECRET_ 前缀"
		case errors.Is(e, credential.ErrUnavailable):
			status, code, msg = 409, "CREDENTIAL_UNAVAILABLE", "凭据不可用、已过期或超出用户/目标范围；检查加密密钥和环境引用"
		case errors.Is(e, v22_credential_repo.ErrConflict):
			status, code, msg = 409, "CREDENTIAL_CONFLICT", "版本/请求已变化或容量达到上限，本地编辑保留；请读取核对"
		case errors.Is(e, v22_credential_repo.ErrNotFound):
			status, code, msg = 404, "NOT_FOUND", "凭据不存在"
		case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
			status, code, msg = 408, "CREDENTIAL_TIMEOUT", "请求未确认，请读取凭据核对，勿自动重复提交"
		}
		fail(w, r, status, code, msg, status >= 500 || status == 408, "credential", "")
		return
	}
	if r.Method == "POST" && mux.Vars(r)["credential_id"] == "" {
		created(w, r, v)
	} else {
		ok(w, r, v)
	}
}
func Credentials(w http.ResponseWriter, r *http.Request) {
	var i credential.Input
	if r.Method == "POST" && credentialBody(w, r, &i) != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "凭据请求无效", false, "credential", "")
		return
	}
	credentialRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		if r.Method == "POST" {
			return v22_credential_repo.Save(ctx, owner, i, true)
		}
		return v22_credential_repo.List(ctx, owner)
	})
}
func CredentialItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["credential_id"]
	if _, e := uuid.Parse(id); e != nil {
		fail(w, r, 400, "INVALID_ARGUMENT", "凭据ID无效", false, "credential", "")
		return
	}
	var i credential.Input
	if r.Method == "PATCH" {
		if credentialBody(w, r, &i) != nil || i.ID != id {
			fail(w, r, 400, "INVALID_ARGUMENT", "凭据修改无效", false, "credential", "")
			return
		}
	}
	revision := 0
	if r.Method == "DELETE" {
		revision, _ = strconv.Atoi(r.URL.Query().Get("expected_revision"))
		if revision < 1 {
			fail(w, r, 400, "INVALID_ARGUMENT", "需要凭据 revision", false, "credential", "")
			return
		}
	}
	credentialRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
		switch r.Method {
		case "PATCH":
			return v22_credential_repo.Save(ctx, owner, i, false)
		case "DELETE":
			return v22_credential_repo.Revoke(ctx, owner, id, revision)
		default:
			return v22_credential_repo.Get(ctx, owner, id)
		}
	})
}

// Checks are read-only runtime/scope checks, not an assertion that the website logged in.
func CredentialCheck(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["credential_id"]
		var i struct {
			TargetURL string `json:"target_url"`
			Kind      string `json:"kind"`
		}
		if _, e := uuid.Parse(id); e != nil || credentialBody(w, r, &i) != nil {
			fail(w, r, 400, "INVALID_ARGUMENT", "检查请求无效", false, "credential", "")
			return
		}
		credentialRequest(w, r, func(ctx context.Context, owner int64) (any, error) {
			row, _, e := v22_credential_repo.Resolve(ctx, owner, i.TargetURL, credential.Ref(id), i.Kind)
			if e != nil {
				return nil, e
			}
			ready := true
			if i.Kind == "browser_cookie" {
				ready = browser != nil && browser.ProbeEditorAuthorization(ctx) == nil
			}
			return map[string]any{"credential_id": id, "revision": row.Revision, "runtime_ready": ready, "website_authorization": "unverified", "network_accessed": false, "checked_at": time.Now()}, nil
		})
	}
}
