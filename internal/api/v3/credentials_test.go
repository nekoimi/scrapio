package v3

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestD04CredentialReadRequiresOwner(t *testing.T) {
	w := httptest.NewRecorder()
	Credentials(w, httptest.NewRequest("GET", "/credentials", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestD04CredentialBodiesBounded(t *testing.T) {
	for _, raw := range []string{`{"unknown":"secret"}`, `{} {}`, `{"secret":"` + strings.Repeat("x", 17000) + `"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		var input struct {
			Secret string `json:"secret"`
		}
		if credentialBody(w, r, &input) == nil {
			t.Fatal("invalid body accepted")
		}
	}
}
