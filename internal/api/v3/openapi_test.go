package v3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/db/table"
	"go.yaml.in/yaml/v3"
)

func TestA02A03OpenAPIContract(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	for path, item := range paths {
		for method, value := range item.(map[string]any) {
			if method != "get" && method != "post" && method != "put" && method != "delete" {
				continue
			}
			op := value.(map[string]any)
			if op["x-implementation-status"] != "implemented" {
				continue
			}
			for code, value := range op["responses"].(map[string]any) {
				if code != "200" && code != "201" {
					continue
				}
				response := value.(map[string]any)
				content, has := response["content"].(map[string]any)
				if !has {
					continue
				}
				media, has := content["application/json"].(map[string]any)
				if !has {
					continue
				}
				schema := media["schema"].(map[string]any)
				if ref, ok := schema["$ref"].(string); ok {
					var target any = doc
					for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
						mapping, ok := target.(map[string]any)
						if !ok {
							t.Fatalf("invalid schema reference %s", ref)
						}
						target = mapping[part]
					}
					resolved, ok := target.(map[string]any)
					if !ok {
						t.Fatalf("missing schema reference %s", ref)
					}
					schema = resolved
				}
				properties, ok := schema["properties"].(map[string]any)
				if !ok {
					t.Fatalf("%s %s %s missing envelope properties", method, path, code)
				}
				if properties["data"] == nil || properties["request_id"] == nil {
					t.Fatalf("%s %s %s is missing success envelope", method, path, code)
				}
			}
		}
	}
	if paths["/browser-sessions/{session_id}/heartbeat"] == nil {
		t.Fatal("heartbeat contract missing")
	}
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	properties := schemas["SessionResponse"].(map[string]any)["properties"].(map[string]any)
	payload, err := json.Marshal(sessionDTO(&table.V22BrowserSession{Status: "ready", ExpiresAt: time.Now().Add(time.Minute)}))
	if err != nil {
		t.Fatal(err)
	}
	var dto map[string]any
	if err := json.Unmarshal(payload, &dto); err != nil {
		t.Fatal(err)
	}
	for field := range dto {
		if properties[field] == nil {
			t.Fatalf("SessionResponse omits DTO field %s", field)
		}
	}
	for field := range properties {
		if _, exists := dto[field]; !exists {
			t.Fatalf("SessionResponse advertises absent field %s", field)
		}
	}
	if len(paths["/browser-sessions/{session_id}/events"].(map[string]any)["parameters"].([]any)) != 1 {
		t.Fatal("SSE must not promise historical replay")
	}
}
