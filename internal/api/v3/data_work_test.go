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

func TestDataWorkDTOAndOpenAPI(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, dto := range map[string]any{"DataView": viewDTO(table.V22DataView{Query: `{}`}), "DataExport": exportDTO(&table.V22Export{Query: `{}`, Schema: `{}`, File: []byte("DO_NOT_EXPOSE_FILE"), LeaseToken: "DO_NOT_EXPOSE_TOKEN"})} {
		payload, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "DO_NOT_EXPOSE") {
			t.Fatal("export internals leaked")
		}
		var fields map[string]any
		if err = json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		props := schemas[name].(map[string]any)["properties"].(map[string]any)
		for field := range fields {
			if props[field] == nil {
				t.Fatalf("%s missing %s", name, field)
			}
		}
		for field := range props {
			if _, ok := fields[field]; !ok {
				t.Fatalf("%s advertises absent %s", name, field)
			}
		}
	}
	paths := doc["paths"].(map[string]any)
	if paths["/exports/by-key"] == nil || paths["/exports/{export_id}/cancel"] == nil {
		t.Fatal("unknown-request recovery or cancellation contract missing")
	}
}
