package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nekoimi/scrapio/internal/api/middleware"
)

func TestConflictIncludesLatestDraftAndRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-test")
	r := httptest.NewRequest(http.MethodPut, "/api/v3/collectors/42/draft", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	conflict(w, r, map[string]any{"id": "42", "revision": 3})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d", w.Code)
	}
	var response struct {
		Error struct {
			Code   string `json:"code"`
			Latest struct {
				ID       string `json:"id"`
				Revision int    `json:"revision"`
			} `json:"latest"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "STALE_REVISION" || response.Error.Latest.ID != "42" || response.Error.Latest.Revision != 3 || response.RequestID != "req-test" {
		t.Fatalf("unexpected conflict payload: %+v", response)
	}
}

func TestSuccessEnvelopeIncludesRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-ok")
	w := httptest.NewRecorder()
	ok(w, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx), map[string]string{"status": "ready"})
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["request_id"] != "req-ok" || response["data"] == nil {
		t.Fatalf("unexpected envelope: %v", response)
	}
}
