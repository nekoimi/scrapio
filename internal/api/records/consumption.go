package records

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
)

func filterFromQuery(r *http.Request) (record_repo.Filter, error) {
	q := r.URL.Query()
	id, err := strconv.ParseInt(q.Get("dataset_id"), 10, 64)
	if err != nil {
		return record_repo.Filter{}, err
	}
	f := record_repo.Filter{DatasetID: id, Query: q.Get("query"), Activity: q.Get("activity")}
	if q.Get("source_id") != "" {
		f.SourceID, err = strconv.ParseInt(q.Get("source_id"), 10, 64)
		if err != nil {
			return f, err
		}
	}
	if q.Get("since") != "" {
		date, e := time.Parse(time.RFC3339, q.Get("since"))
		if e != nil {
			return f, e
		}
		f.Since = &date
	}
	return f, f.Validate()
}
func filterFromBody(w http.ResponseWriter, r *http.Request) (record_repo.Filter, error) {
	var f record_repo.Filter
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&f)
	if err != nil {
		return f, err
	}
	return f, f.Validate()
}

func Coverage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("dataset_id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	rows, err := record_repo.Coverage(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, rows)
}
func Views(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("dataset_id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	rows, err := record_repo.ListViews(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, rows)
}
func SaveView(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DatasetID int64              `json:"dataset_id"`
		Name      string             `json:"name"`
		Filter    record_repo.Filter `json:"filter"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	row, err := record_repo.SaveView(input.DatasetID, input.Name, input.Filter)
	if err != nil {
		respond.InvalidRecord(w, err.Error())
		return
	}
	respond.Ok(w, row)
}
func DeleteView(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DatasetID int64 `json:"dataset_id"`
		ID        int64 `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := record_repo.DeleteView(input.DatasetID, input.ID); err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, nil)
}

func ExportDirect(w http.ResponseWriter, r *http.Request) {
	f, err := filterFromBody(w, r)
	if err != nil {
		respond.InvalidRecord(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	n, err := record_repo.CountFiltered(ctx, f)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if n > record_repo.DirectExportLimit {
		respond.InvalidRecord(w, "more than 1000 rows; create a background export job")
		return
	}
	data, _, err := record_repo.BuildCSV(ctx, f, record_repo.DirectExportLimit)
	if err != nil {
		respond.Error(w, err)
		return
	}
	sendCSV(w, f.DatasetID, data)
}
func ExportCreate(w http.ResponseWriter, r *http.Request) {
	f, err := filterFromBody(w, r)
	if err != nil {
		respond.InvalidRecord(w, err.Error())
		return
	}
	job, err := record_repo.CreateExportJob(r.Context(), f)
	if err != nil {
		respond.InvalidRecord(w, err.Error())
		return
	}
	respond.Ok(w, job)
}
func ExportStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	job, err := record_repo.GetExportJob(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, job)
}
func ExportDownload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	datasetID, data, err := record_repo.ExportContent(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	sendCSV(w, datasetID, data)
}
func sendCSV(w http.ResponseWriter, datasetID int64, data []byte) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dataset-%d.csv", datasetID))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
