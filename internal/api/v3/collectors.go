package v3

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
)

func owner(r *http.Request) (*table.Admin, bool) {
	sub, ok := r.Context().Value(request.ContextJwtUser).(string)
	if !ok {
		return nil, false
	}
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil || db.Instance() == nil {
		return nil, false
	}
	admin := new(table.Admin)
	has, err := db.Instance().ID(id).Get(admin)
	return admin, err == nil && has
}

func collectorID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["collector_id"], 10, 64)
	return id, err == nil && id > 0
}

func CreateCollector(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	var input v22_collector_repo.CreateInput
	if err := request.Parse(r, &input); err != nil {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "")
		return
	}
	row, err := v22_collector_repo.Create(admin.Id, input, r.Header.Get("Idempotency-Key"))
	if err != nil {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "")
		return
	}
	created(w, r, row)
}

func ListCollectors(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	rows, err := v22_collector_repo.List(admin.Id)
	if err != nil {
		fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
		return
	}
	ok(w, r, map[string]any{"items": rows, "has_more": false})
}

func GetCollectorDraft(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "collector_id is required", false, "validate", "collector_id")
		return
	}
	row, has, err := v22_collector_repo.Get(admin.Id, id)
	if err != nil {
		fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
		return
	}
	if !has {
		fail(w, r, http.StatusNotFound, "NOT_FOUND", "collector not found", false, "lookup", "collector_id")
		return
	}
	ok(w, r, row)
}

func UpdateCollectorDraft(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "collector_id is required", false, "validate", "collector_id")
		return
	}
	var input v22_collector_repo.UpdateInput
	if err := request.Parse(r, &input); err != nil {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "")
		return
	}
	row, err := v22_collector_repo.Update(admin.Id, id, input)
	if err != nil {
		var conflict *v22_collector_repo.RevisionConflict
		if errors.As(err, &conflict) {
			fail(w, r, http.StatusConflict, "STALE_REVISION", "草稿已被其他请求修改，请重新加载", false, "save", "expected_revision")
			return
		}
		status, code := http.StatusBadRequest, "INVALID_ARGUMENT"
		if err.Error() == "collector not found" {
			status, code = http.StatusNotFound, "NOT_FOUND"
		}
		fail(w, r, status, code, err.Error(), false, "save", "")
		return
	}
	ok(w, r, row)
}

func ArchiveCollector(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	id, valid := collectorID(r)
	if !valid {
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "collector_id is required", false, "validate", "collector_id")
		return
	}
	if err := v22_collector_repo.Archive(admin.Id, id); err != nil {
		fail(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), false, "archive", "collector_id")
		return
	}
	ok(w, r, map[string]any{"id": id, "status": "archived"})
}
