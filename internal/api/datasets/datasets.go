package datasets

import (
	"net/http"
	"strconv"

	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
)

type projectRequest struct {
	ID    int64  `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Goal  string `json:"goal"`
	Owner string `json:"owner"`
}

func ProjectUpdate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Goal   string `json:"goal"`
		Owner  string `json:"owner"`
		Status string `json:"status"`
	}
	if err := request.Parse(r, &input); err != nil || input.ID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	row, err := dataset_repo.UpdateProject(input.ID, input.Name, input.Goal, input.Owner, input.Status)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, row)
}

type schemaRequest struct {
	ID int64 `json:"id"`
	dataset_repo.SchemaInput
}

func ProjectList(w http.ResponseWriter, _ *http.Request) {
	rows, err := dataset_repo.ListProjects()
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, rows)
}
func ProjectHealth(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	health, err := dataset_repo.Health(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, health)
}
func ProjectCreate(w http.ResponseWriter, r *http.Request) {
	var input projectRequest
	if err := request.Parse(r, &input); err != nil {
		respond.Error(w, err)
		return
	}
	row, err := dataset_repo.CreateProject(input.Code, input.Name, input.Goal, input.Owner)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, row)
}
func List(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	rows, err := dataset_repo.ListDatasets(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, rows)
}
func Detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	row, fields, err := dataset_repo.Detail(id, version)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if version <= 0 {
		version = row.SchemaVersion
	}
	respond.Ok(w, map[string]any{"dataset": row, "schema_version": version, "fields": fields})
}
func Create(w http.ResponseWriter, r *http.Request) {
	var input dataset_repo.CreateDatasetInput
	if err := request.Parse(r, &input); err != nil {
		respond.Error(w, err)
		return
	}
	row, err := dataset_repo.CreateDataset(input)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, row)
}
func UpdateSchema(w http.ResponseWriter, r *http.Request) {
	var input schemaRequest
	if err := request.Parse(r, &input); err != nil || input.ID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	row, err := dataset_repo.UpdateSchema(input.ID, input.SchemaInput)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, row)
}
