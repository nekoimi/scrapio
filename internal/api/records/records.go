package records

import (
	"net/http"
	"strconv"

	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
)

type createRequest struct {
	DatasetID      int64          `json:"dataset_id"`
	Values         map[string]any `json:"values"`
	SourceID       *int64         `json:"source_id,omitempty"`
	SourceURL      string         `json:"source_url,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

func ReconcileLegacy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AfterID int64 `json:"after_id"`
		Limit   int   `json:"limit"`
	}
	if err := request.Parse(r, &input); err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	report, err := record_repo.ReconcileLegacy(input.AfterID, input.Limit)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, report)
}

func Create(w http.ResponseWriter, r *http.Request) {
	var input createRequest
	if err := request.Parse(r, &input); err != nil || input.DatasetID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	result, err := record_repo.Save(record_repo.Candidate{DatasetID: input.DatasetID, Values: input.Values, SourceID: input.SourceID, SourceURL: input.SourceURL, IdempotencyKey: input.IdempotencyKey})
	if err != nil {
		if result.Decision == "invalid" || result.Decision == "conflict" {
			respond.InvalidRecord(w, err.Error())
			return
		}
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}
func List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	id, err := strconv.ParseInt(query.Get("dataset_id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	page, _ := strconv.Atoi(query.Get("page"))
	size, _ := strconv.Atoi(query.Get("size"))
	f, err := filterFromQuery(r)
	if err != nil {
		respond.InvalidRecord(w, err.Error())
		return
	}
	rows, total, err := record_repo.ListFiltered(f, page, size)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"list": rows, "total": total})
}
func Detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	row, observations, revisions, err := record_repo.Detail(id, limit)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"record": row, "observations": observations, "revisions": revisions})
}
