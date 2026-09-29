package server

import (
	"net/http/httptest"
	"testing"
)

func TestAppEntryDeepLink(t *testing.T) {
	for _, tc := range []struct{ path, location string }{
		{"/app", "/#/app/home"},
		{"/app/", "/#/app/home"},
		{"/app/collectors?filter=draft", "/#/app/collectors?filter=draft"},
	} {
		w := httptest.NewRecorder()
		appEntry(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != 302 || w.Header().Get("Location") != tc.location {
			t.Errorf("%s: status %d, location %q", tc.path, w.Code, w.Header().Get("Location"))
		}
	}
}
