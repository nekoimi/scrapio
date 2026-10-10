package v3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/repo/v22_governance_repo"
)

func TestD05GovernanceRequiresOwner(t *testing.T) {
	for _, h := range []http.HandlerFunc{TableSchemaChanges, RetentionSettings, RetentionCleanup, OperationLogs} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 401 {
			t.Fatal("owner isolation missing", w.Code)
		}
	}
	called := false
	h := OperationAudit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/credentials", strings.NewReader(`{"secret":"must-not-log"}`)))
	if called || w.Code != 401 || strings.Contains(w.Body.String(), "must-not-log") {
		t.Fatal("unauthenticated mutation leaked or executed")
	}
}
func TestD05OperationWriterAndReadPassthrough(t *testing.T) {
	w := httptest.NewRecorder()
	r := &operationWriter{ResponseWriter: w}
	r.WriteHeader(201)
	r.WriteHeader(500)
	_, _ = r.Write([]byte("ok"))
	if w.Code != 201 || r.status != 201 || w.Body.String() != "ok" {
		t.Fatal("audit changed response contract")
	}
	called := false
	OperationAudit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("read path unexpectedly audits")
	}
}
func TestD05CapacityAndConflictAreExplicit(t *testing.T) {
	for _, err := range []error{v22_governance_repo.ErrCapacity, v22_governance_repo.ErrConflict} {
		w := httptest.NewRecorder()
		governanceError(w, httptest.NewRequest("POST", "/", nil), err)
		if w.Code != 409 {
			t.Fatal(w.Code)
		}
	}
}
