package v3

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectionFailure(t *testing.T) {
	// Input mistakes must not become retryable service outages; stale evidence
	// must stay a conflict. Unknown remote failures must not expose exception text.
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"NO_ELEMENT", 400}, {"INVALID_LOCATOR", 400}, {"OUTSIDE_RECORD_SCOPE", 400},
		{"RECORD_SCOPE_EMPTY", 400}, {"INVALID_ARGUMENT", 400}, {"INSPECTION_LIMIT", 400},
		{"ELEMENT_REFERENCE_STALE", 409}, {"STALE_PAGE_STATE", 409}, {"INSPECTION_BUSY", 409},
		{"SESSION_GONE", 410}, {"INSPECTION_FAILED", 503}, {"remote-stack-secret", 503},
	} {
		t.Run(tc.code, func(t *testing.T) {
			status, message := inspectionFailure(tc.code)
			w := httptest.NewRecorder()
			fail(w, httptest.NewRequest("POST", "/inspect", nil), status, tc.code, message, status == 503, "inspect", "")
			var response errorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || response.Error.Code != tc.code || response.Error.Retryable != (tc.status == 503) {
				t.Fatalf("unexpected error contract: status=%d error=%+v", w.Code, response.Error)
			}
			if response.Error.Message == "" || strings.Contains(response.Error.Message, "secret") || strings.Contains(response.Error.Message, "browser job") {
				t.Fatal("inspection response leaked remote details or omitted recovery guidance")
			}
		})
	}
}
