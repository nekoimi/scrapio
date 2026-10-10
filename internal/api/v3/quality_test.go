package v3

import (
	"net/http/httptest"
	"testing"
)

func TestQualityReadRejectsInvalidFilter(t *testing.T) {
	for _, url := range []string{"/?status=invalid", "/?limit=51&collector_id=1", "/?limit=0", "/?collector_id=-1", "/?cursor=invalid"} {
		w := httptest.NewRecorder()
		QualityIssues(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 400 {
			t.Fatalf("%s returned %d", url, w.Code)
		}
	}
	w := httptest.NewRecorder()
	QualityIssues(w, httptest.NewRequest("GET", "/?status=active", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated read accepted")
	}
}
