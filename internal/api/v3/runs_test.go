package v3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nekoimi/scrapio/internal/db/table"
	"go.yaml.in/yaml/v3"
)

func TestFormalRunDTOAndListContract(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if yaml.Unmarshal(raw, &contract) != nil {
		t.Fatal("invalid YAML")
	}
	schemas := contract["components"].(map[string]any)["schemas"].(map[string]any)
	properties := schemas["FormalRun"].(map[string]any)["properties"].(map[string]any)
	row := &table.V22Run{Input: `{"version_id":"one"}`, Summary: `{"status":"succeeded","committed":true,"counts":{"unchanged":1},"writes":[{"record_id":"one"}],"output":{"rows":[{}]}}`}
	for field := range runDTO(row) {
		if properties[field] == nil {
			t.Fatalf("run DTO field %s missing in contract", field)
		}
	}
	var result map[string]any
	data, _ := json.Marshal(runListDTO(row))
	if json.Unmarshal(data, &result) != nil {
		t.Fatal("invalid DTO")
	}
	summary := result["summary"].(map[string]any)
	if summary["committed"] != true || summary["writes"] != nil || summary["output"] != nil {
		t.Fatal("list must preserve commit result without heavy output", summary)
	}
	for _, path := range []string{"/collectors/{collector_id}/runs", "/runs", "/runs/by-key", "/runs/{run_id}", "/runs/{run_id}/results", "/runs/{run_id}/events", "/runs/{run_id}/cancel"} {
		if contract["paths"].(map[string]any)[path] == nil {
			t.Fatalf("missing run path %s", path)
		}
	}
}
