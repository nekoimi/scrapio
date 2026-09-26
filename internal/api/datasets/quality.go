package datasets

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/audit_repo"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
)

func WorkflowQuality(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("workflow_id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	result, err := dataset_repo.Quality(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}

func SaveWorkflowQuality(w http.ResponseWriter, r *http.Request) {
	var input dataset_repo.QualityThreshold
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := dataset_repo.SaveQualityThreshold(input); err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.quality_threshold_saved", "workflow", &input.WorkflowID, input)
	respond.Ok(w, input)
}
