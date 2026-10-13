package v4

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCOStrictBodyRejectsUnknownAndTrailing(t *testing.T) {
	for _, body := range []string{`{"name":"x","detail_fields":[]}`, `{"name":"x"} {}`, `null`} {
		r := httptest.NewRequest("POST", "/api/v4/collectors", strings.NewReader(body))
		w := httptest.NewRecorder()
		var input struct {
			Name string `json:"name"`
		}
		if decode(w, r, &input) || w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
}
