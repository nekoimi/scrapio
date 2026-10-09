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
		if errors.Is(err, v22_collector_repo.ErrIdempotencyConflict) {
			fail(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error(), false, "create", "Idempotency-Key")
			return
		}
		fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "")
		return
	}
	created(w, r, v22_collector_repo.ToDTO(row))
}

func ListCollectors(w http.ResponseWriter, r *http.Request) {
	admin, authenticated := owner(r)
	if !authenticated {
		fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "身份认证异常", false, "auth", "")
		return
	}
	limit := 25
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "limit must be between 1 and 100", false, "validate", "limit")
			return
		}
		limit = parsed
	}
	page, err := v22_collector_repo.List(admin.Id, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		if err.Error() == "invalid cursor" || err.Error() == "limit must be between 1 and 100" {
			fail(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), false, "validate", "cursor")
		} else {
			fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "storage", "")
		}
		return
	}
	items := make([]v22_collector_repo.Collector, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, v22_collector_repo.ToDTO(&page.Items[i]))
	}
	ok(w, r, map[string]any{"items": items, "has_more": page.HasMore, "next_cursor": page.NextCursor})
}

func CopyCollector(w http.ResponseWriter, r *http.Request) {
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
	row, err := v22_collector_repo.Copy(admin.Id, id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		if errors.Is(err, v22_collector_repo.ErrIdempotencyConflict) {
			fail(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error(), false, "copy", "Idempotency-Key")
		} else if err.Error() == "collector not found" {
			fail(w, r, http.StatusNotFound, "NOT_FOUND", err.Error(), false, "copy", "collector_id")
		} else {
			fail(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), true, "copy", "")
		}
		return
	}
	created(w, r, v22_collector_repo.ToDTO(row))
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
	ok(w, r, v22_collector_repo.ToDTO(row))
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
	row, changed, err := v22_collector_repo.Update(admin.Id, id, input)
	if err != nil {
		var conflict *v22_collector_repo.RevisionConflict
		if errors.As(err, &conflict) {
			v3Conflict(w, r, v22_collector_repo.ToDTO(conflict.Latest))
			return
		}
		status, code := http.StatusBadRequest, "INVALID_ARGUMENT"
		if err.Error() == "collector not found" {
			status, code = http.StatusNotFound, "NOT_FOUND"
		}
		fail(w, r, status, code, err.Error(), false, "save", "")
		return
	}
	dto := v22_collector_repo.ToDTO(row)
	dto.SaveSummary = &v22_collector_repo.SaveSummary{Revision: row.Revision, ChangedFields: changed, SavedAt: row.UpdatedAt}
	ok(w, r, dto)
}

func ValidateCollectorDraft(w http.ResponseWriter, r *http.Request) {
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
	row, result, err := v22_collector_repo.Validate(admin.Id, id)
	if err != nil {
		status, code := http.StatusInternalServerError, "INTERNAL"
		if err.Error() == "collector not found" {
			status, code = http.StatusNotFound, "NOT_FOUND"
		}
		fail(w, r, status, code, err.Error(), status >= 500, "validate", "")
		return
	}
	ok(w, r, map[string]any{"collector": v22_collector_repo.ToDTO(row), "validation": result})
}

func v3Conflict(w http.ResponseWriter, r *http.Request, latest any) {
	conflict(w, r, latest)
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
	ok(w, r, map[string]any{"id": strconv.FormatInt(id, 10), "status": "archived"})
}
