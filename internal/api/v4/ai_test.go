package v4

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/co"
)

func TestCOAIPathsRequireOwner(t *testing.T) {
	router := mux.NewRouter()
	Mount(router.PathPrefix("/api").Subrouter(), nil, nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/ai-capabilities"}, {"POST", "/ai/prepare"}, {"GET", "/ai/suggestions"}, {"GET", "/ai/suggestions/fixture"}, {"DELETE", "/ai/suggestions/fixture"},
		{"POST", "/ai/suggestions/fixture/call"}, {"POST", "/ai/suggestions/fixture/validate"}, {"POST", "/ai/suggestions/fixture/accept"}, {"POST", "/ai/suggestions/fixture/apply"}, {"POST", "/ai/suggestions/fixture/reject"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/v4"+tc.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatal(tc, w.Code)
		}
	}
}
func TestCOAIControlCannotInjectCandidate(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"confirmed":true,"selectors":[{"field":"id"}]}`))
	var p co.AIControlRequest
	if decode(w, r, &p) || w.Code != 400 {
		t.Fatal("client candidate accepted")
	}
}
