package v3

import (
	"encoding/json"
	"net/http"

	"github.com/nekoimi/scrapio/internal/api/middleware"
)

type apiResponse struct {
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Stage     string `json:"stage,omitempty"`
	Path      string `json:"path,omitempty"`
	Latest    any    `json:"latest,omitempty"`
}

type errorResponse struct {
	Error     apiError `json:"error"`
	RequestID string   `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ok(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusOK, apiResponse{Data: data, RequestID: middleware.RequestID(r.Context())})
}

func created(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusCreated, apiResponse{Data: data, RequestID: middleware.RequestID(r.Context())})
}

func accepted(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusAccepted, apiResponse{Data: data, RequestID: middleware.RequestID(r.Context())})
}

func fail(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool, stage, path string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message, Retryable: retryable, Stage: stage, Path: path}, RequestID: middleware.RequestID(r.Context())})
}

func conflict(w http.ResponseWriter, r *http.Request, latest any) {
	writeJSON(w, http.StatusConflict, errorResponse{
		Error:     apiError{Code: "STALE_REVISION", Message: "草稿已被其他请求修改；本地修改已保留，请选择加载服务器版本", Retryable: false, Stage: "save", Path: "expected_revision", Latest: latest},
		RequestID: middleware.RequestID(r.Context()),
	})
}
