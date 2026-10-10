package v3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/db/table"
	"go.yaml.in/yaml/v3"
)

func TestC04RunControlsAndEvidenceContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err = yaml.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	schemas := contract["components"].(map[string]any)["schemas"].(map[string]any)
	parent := "original"
	data, err := json.Marshal(runDTO(&table.V22Run{Status: "partial", RetryOf: &parent, RetryScope: "full_run", Input: `{}`, Summary: `{}`, LeaseToken: "DO_NOT_EXPOSE_TOKEN", SessionId: "DO_NOT_EXPOSE_SESSION", Definition: `DO_NOT_EXPOSE_DEFINITION`}))
	if err != nil || strings.Contains(string(data), "DO_NOT_EXPOSE") {
		t.Fatal("run control read leaked execution internals", err)
	}
	var dto map[string]any
	if err = json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	controls := dto["controls"].(map[string]any)
	if controls["can_retry"] != true || controls["can_cancel"] != false || dto["retry_of"] != parent {
		t.Fatal("incorrect terminal controls or lineage", dto)
	}
	props := schemas["RunControls"].(map[string]any)["properties"].(map[string]any)
	for field := range controls {
		if props[field] == nil {
			t.Fatal("missing control contract", field)
		}
	}
	for _, name := range []string{"RunAttemptsRead", "PageAttemptsRead", "RunDiagnosticPage"} {
		p := schemas[name].(map[string]any)["properties"].(map[string]any)
		if p["has_more"] == nil || p["next_cursor"] == nil {
			t.Fatal("unbounded evidence contract", name)
		}
	}
	paths := contract["paths"].(map[string]any)
	for _, path := range []string{"/runs/statistics", "/runs/{run_id}/retries", "/runs/{run_id}/diagnostics", "/runs/{run_id}/coverage"} {
		if paths[path] == nil {
			t.Fatal("missing run center path", path)
		}
	}
}
