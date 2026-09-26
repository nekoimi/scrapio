package documents

import (
	"net/http"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
)

func RetentionPreview(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 3650 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	result, err := task_repo.PreviewDocumentRetention(time.Now().AddDate(0, 0, -days))
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}

func List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if value := r.URL.Query().Get("document_type"); value != "" {
		s.Where("document_type = ?", value)
	}
	total, err := s.Count(new(table.Document))
	if err != nil {
		respond.Error(w, err)
		return
	}
	s = db.Instance().NewSession()
	defer s.Close()
	if value := r.URL.Query().Get("document_type"); value != "" {
		s.Where("document_type = ?", value)
	}
	rows := make([]table.Document, 0)
	if err := s.Cols("id", "task_id", "document_type", "content_hash", "content_size", "metadata", "created_at").Desc("created_at").Limit(size, (page-1)*size).Find(&rows); err != nil {
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
	document := new(table.Document)
	has, err := db.Instance().ID(id).Get(document)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	assets := make([]table.DocumentAsset, 0)
	if err := db.Instance().Where("document_id = ?", id).Cols("id", "document_id", "asset_type", "content_type", "content", "content_hash", "content_size", "metadata", "created_at").Find(&assets); err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"document": document, "assets": assets})
}
