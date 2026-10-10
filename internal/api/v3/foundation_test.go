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

func TestC05HomeRequiresOwnerBeforeAnyDatabaseRead(t *testing.T) {
	for _, handler := range []func(http.ResponseWriter, *http.Request){Home, CollectorHealthList} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 401 {
			t.Fatalf("unauthenticated overview exposed: %d", w.Code)
		}
	}
}
