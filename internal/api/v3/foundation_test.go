package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFoundationDoesNotExposeLegacyCountsOrUnsupportedEditor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
		check  func(t *testing.T, data map[string]any)
	}{
		{"capabilities", Capabilities, func(t *testing.T, data map[string]any) {
			if data["browser_ready"] != false || data["supports_live_inspection"] != false {
				t.Fatal("interactive browser advertised before protocol delivery")
			}
		}},
		{"home", Home, func(t *testing.T, data map[string]any) {
			if data["status"] != "pending" || data["records"] != nil {
				t.Fatal("home must not present legacy records as v2.2 data")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handle(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 200 {
				t.Fatalf("status = %d", w.Code)
			}
			var response struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			tc.check(t, response.Data)
		})
	}
}
