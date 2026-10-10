package v3

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/db/table"
	"go.yaml.in/yaml/v3"
)

func TestScheduleAPIRejectsPrivilegeOverrides(t *testing.T) {
	token := "Bearer sap_" + strings.Repeat("a", 64)
	for _, body := range []string{`{"version_id":"other"}`, `{"allowed_origins":["https://other.org"]}`, `{"definition":{}}`, `{"budget":{"seconds":30},"unexpected":1}`, `{"budget":{"seconds":30,"pages":2,"records":10}}`, `{"budget":{"seconds":30,"pages":2,"records":10,"details":null}}`} {
		req := httptest.NewRequest("POST", "/api/v3/collectors/1/api-runs", strings.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		req = mux.SetURLVars(req, map[string]string{"collector_id": "1"})
		w := httptest.NewRecorder()
		APIRun(w, req)
		if w.Code != 400 {
			t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/api/v3/collectors/1/api-runs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	APIRun(w, req)
	if w.Code != 401 {
		t.Fatal("missing token accepted")
	}
}
func TestScheduleDTOHidesCredentialHash(t *testing.T) {
	raw, err := json.Marshal(keyDTO(table.V22APIKey{Id: "id", Input: `{}`, TokenHash: "SHOULD_NOT_LEAK"}))
	if err != nil || strings.Contains(string(raw), "SHOULD_NOT_LEAK") {
		t.Fatal("credential hash leaked")
	}
	if scheduleDTO(nil) != nil {
		t.Fatal("absent schedule must be null")
	}
}

func TestScheduleOpenAPIContract(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for name, dto := range map[string]any{"CollectorSchedule": scheduleDTO(&table.V22Schedule{Input: `{}`}), "CollectorAPIKey": keyDTO(table.V22APIKey{Input: `{}`})} {
		data, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err = json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		props := schemas[name].(map[string]any)["properties"].(map[string]any)
		for field := range fields {
			if props[field] == nil {
				t.Fatalf("%s missing field %s", name, field)
			}
		}
		for field := range props {
			if _, has := fields[field]; !has {
				t.Fatalf("%s advertises absent field %s", name, field)
			}
		}
	}
	paths := doc["paths"].(map[string]any)
	op := paths["/collectors/{collector_id}/api-runs"].(map[string]any)["post"].(map[string]any)
	security := op["security"].([]any)[0].(map[string]any)
	if _, has := security["collectorTrigger"]; !has {
		t.Fatal("API trigger advertises broad administrator authentication")
	}
	if components["securitySchemes"].(map[string]any)["collectorTrigger"] == nil {
		t.Fatal("API credential scheme missing")
	}
	values := schemas["FormalRun"].(map[string]any)["properties"].(map[string]any)["trigger_source"].(map[string]any)["enum"].([]any)
	if len(values) != 3 {
		t.Fatal("manual/schedule/api source contract missing")
	}
}
