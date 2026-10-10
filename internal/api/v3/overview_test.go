package v3

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/overview"
	"go.yaml.in/yaml/v3"
)

func TestC05HealthRejectsInvalidReadsBeforeStorage(t *testing.T) {
	for _, url := range []string{"/?attention=maybe", "/?cursor=0", "/?cursor=x", "/?limit=51"} {
		w := httptest.NewRecorder()
		CollectorHealthList(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 400 {
			t.Fatalf("invalid query %s: %d", url, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := mux.SetURLVars(httptest.NewRequest("GET", "/", nil), map[string]string{"collector_id": "1"})
	CollectorHealth(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated health detail: %d", w.Code)
	}
}

func TestC05OverviewContractAndReadOnlyPayload(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	paths := doc["paths"].(map[string]any)
	for path, name := range map[string]string{"/home": "HomeOverview", "/collectors/health": "HealthPage", "/collectors/{collector_id}/health": "CollectorHealth"} {
		op := paths[path].(map[string]any)["get"].(map[string]any)
		responses := op["responses"].(map[string]any)
		for _, code := range []string{"401", "408", "500"} {
			if responses[code] == nil {
				t.Fatalf("%s omits %s; errors must not masquerade as healthy/empty", path, code)
			}
		}
		content := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
		props := content["schema"].(map[string]any)["properties"].(map[string]any)
		if props["data"].(map[string]any)["$ref"] != "#/components/schemas/"+name || props["request_id"] == nil {
			t.Fatal("overview envelope mismatch", path)
		}
	}
	for name, value := range map[string]any{
		"HomeOverview": overview.Home{}, "CollectorHealth": overview.Health{},
		"OverviewRun": overview.Run{}, "HealthSchedule": overview.Schedule{},
		"HomeTable": overview.Table{}, "HealthIssue": overview.Issue{},
	} {
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err = json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		props := schemas[name].(map[string]any)["properties"].(map[string]any)
		if len(fields) != len(props) {
			t.Fatal("DTO/schema field count mismatch", name)
		}
		for field := range fields {
			if props[field] == nil {
				t.Fatal("DTO field absent from contract", name, field)
			}
		}
		for _, forbidden := range []string{"definition", "input", "output_schema", "values", "lease_token", "session_id", "credentials", "content"} {
			if _, exists := fields[forbidden]; exists {
				t.Fatal("overview exposes execution input or internal data", name, forbidden)
			}
		}
	}
	home := schemas["HomeOverview"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"counts", "activity"} {
		for key, value := range home[field].(map[string]any)["properties"].(map[string]any) {
			if value.(map[string]any)["type"] != "string" {
				t.Fatal("aggregate count loses decimal precision", field, key)
			}
		}
	}
	params := paths["/runs"].(map[string]any)["get"].(map[string]any)["parameters"].([]any)
	for _, wanted := range []string{"committed", "since", "until"} {
		found := false
		for _, p := range params {
			if p.(map[string]any)["name"] == wanted {
				found = true
			}
		}
		if !found {
			t.Fatal("home metric cannot preserve completion window", wanted)
		}
	}
}
