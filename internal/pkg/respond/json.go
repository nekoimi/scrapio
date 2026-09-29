package respond

import (
	"encoding/json"
	"net/http"

	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	log "github.com/sirupsen/logrus"
)

type V3ErrorResponse struct {
	Error     V3ErrorDetail `json:"error"`
	RequestID string        `json:"request_id,omitempty"`
}

type V3ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Stage     string `json:"stage,omitempty"`
	Path      string `json:"path,omitempty"`
}

// V3Error writes the stable v2.2 error envelope without changing legacy APIs.
func V3Error(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(V3ErrorResponse{Error: V3ErrorDetail{Code: code, Message: message, Retryable: retryable}, RequestID: requestID(r)})
}

func requestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value, ok := r.Context().Value("request_id").(string); ok {
		return value
	}
	return r.Header.Get("X-Request-ID")
}

type JsonResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data any    `json:"data,omitempty"`
}

func sendJsonResponse(w http.ResponseWriter, response JsonResponse) {
	bs, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("Marshal json error: %s", err.Error())
		return
	}

	_, err = w.Write(bs)
	if err != nil {
		log.Errorf("Send json response error: %s", err.Error())
	}
}

func Ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	sendJsonResponse(w, JsonResponse{
		Code: 0,
		Msg:  "OK",
		Data: data,
	})
}

func Error(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := JsonResponse{
		Code: 500,
		Msg:  err.Error(),
	}
	switch err.(type) {
	case error_ext.CodeError:
		ext := err.(error_ext.CodeError)
		w.WriteHeader(ext.GetHttpStatus())
		resp.Code = ext.GetCode()
		resp.Msg = ext.Error()
	case error:
		w.WriteHeader(http.StatusInternalServerError)
		resp.Code = http.StatusInternalServerError
		resp.Msg = err.Error()
	}
	sendJsonResponse(w, resp)
}

// InvalidDefinition returns a machine-readable publication blocker.
func InvalidDefinition(w http.ResponseWriter, path, reason string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	sendJsonResponse(w, JsonResponse{
		Code: http.StatusUnprocessableEntity,
		Msg:  "workflow definition cannot be published",
		Data: map[string]any{"issues": []map[string]string{{"path": path, "reason": reason}}},
	})
}

func InvalidRecord(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	sendJsonResponse(w, JsonResponse{Code: http.StatusUnprocessableEntity, Msg: "record candidate is invalid", Data: map[string]any{"reason": reason}})
}
