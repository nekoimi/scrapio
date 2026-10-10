package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nekoimi/scrapio/internal/repair"
	"go.yaml.in/yaml/v3"
)

func TestD01RepairReadsRequireOwnerAndCreationConfirmation(t *testing.T) {
	id := "5d4149d8-4d81-42f7-8e1f-872466edbba2"
	for _, handler := range []http.HandlerFunc{PrepareRepair, GetRepairDraft} {
		w := httptest.NewRecorder()
		r := mux.SetURLVars(httptest.NewRequest("GET", "/", nil), map[string]string{"run_id": id, "repair_id": id})
		handler(w, r)
		if w.Code != 401 {
			t.Fatalf("unowned read: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := mux.SetURLVars(httptest.NewRequest("POST", "/", strings.NewReader(`{"run_id":"`+id+`","mode":"continue","expected_revision":1,"confirmed":false}`)), map[string]string{"collector_id": "1"})
	r.Header.Set("Idempotency-Key", "one")
	CreateRepairDraft(w, r)
	if w.Code != 400 {
		t.Fatal("unconfirmed repair", w.Code)
	}
}

func TestD01RepairContractHasRecoveryAndNoRawPayload(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "项目文档v2.2", "阶段0接口契约草案.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if yaml.Unmarshal(raw, &doc) != nil {
		t.Fatal("invalid OpenAPI")
	}
	paths := doc["paths"].(map[string]any)
	for _, p := range []string{"/runs/{run_id}/repair-context", "/collectors/{collector_id}/repair-drafts", "/repair-drafts/by-key", "/repair-drafts/{repair_id}"} {
		if paths[p] == nil {
			t.Fatal("missing repair/recovery path", p)
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["RepairContext"].(map[string]any)["properties"].(map[string]any)
	b, _ := json.Marshal(repair.Context{})
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if len(props) != len(fields) {
		t.Fatal("DTO/contract mismatch")
	}
	for key := range fields {
		if props[key] == nil {
			t.Fatal("missing context field", key)
		}
	}
	for _, key := range []string{"content", "definition", "input", "lease_token", "values", "credentials"} {
		if props[key] != nil {
			t.Fatal("raw data in metadata contract", key)
		}
	}
	for _, name := range []string{"RunDocument", "TrialDocument"} {
		if schemas[name].(map[string]any)["properties"].(map[string]any)["error_code"] == nil {
			t.Fatal("failure document misrepresented as successful", name)
		}
	}
}
