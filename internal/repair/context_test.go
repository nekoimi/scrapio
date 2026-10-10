package repair

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/trial"
)

func TestD01ForkPreservesHistoryAndRequiresFreshOutput(t *testing.T) {
	raw := `{"definition_version":1,"entry_url":"https://example.com","steps":[{"step_id":"items","type":"json_records","config":{"value":9007199254740993}}],"output":{"check_id":"old"}}`
	next, err := ForkDefinition(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next, "output") || !strings.Contains(next, "9007199254740993") || !strings.Contains(raw, "output") {
		t.Fatal("copied confirmation or rounded frozen rule", next)
	}
	if !Compatible(next, "items", "list", "json") || Compatible(next, "items", "detail", "json") || Compatible(next, "removed", "list", "json") {
		t.Fatal("changed step/role incorrectly compatible")
	}
	if len([]rune(ForkName(strings.Repeat("采", 160)))) != 160 {
		t.Fatal("fork name exceeds bound")
	}
}
func TestD01EvidenceDoesNotReplaceMissingOrCorruptInput(t *testing.T) {
	d := &table.V22RunDocument{Content: "[]"}
	meta := &trial.Document{Format: "json", ContentHash: capture.Hash([]byte(d.Content))}
	if Evidence(d, meta) != "available" {
		t.Fatal("empty match still has intact input")
	}
	d.Content = ""
	if Evidence(d, meta) != "missing" {
		t.Fatal("missing input silently available")
	}
	d.Content = "[1]"
	if Evidence(d, meta) != "corrupt" {
		t.Fatal("modified content accepted")
	}
	d.Content = strings.Repeat("x", capture.MaxBytes+1)
	if Evidence(d, meta) != "too_large" {
		t.Fatal("unbounded repair copy")
	}
	if Evidence(nil, meta) != "missing" || Evidence(&table.V22RunDocument{}, nil) != "corrupt" {
		t.Fatal("unavailable metadata fabricated")
	}
	if Eligible("limited") || Eligible("running") || !Eligible("partial") || !Eligible("failed") {
		t.Fatal("repair offered before eligible terminal failure")
	}
}
func TestD01FieldDiagnosticsAreBoundedAndDoNotExposeValues(t *testing.T) {
	d := trial.Document{Result: extraction.Result{Records: []extraction.Record{{Fields: []extraction.FieldResult{{Key: "title", Valid: false, RawValues: []any{"secret"}}, {Key: "title", Valid: false}, {Key: "id", Valid: true}}}}}}
	keys := Fields(d)
	if len(keys) != 1 || keys[0] != "title" {
		t.Fatal(keys)
	}
	h := Context{FieldKeys: keys}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "raw_values") {
		t.Fatal("context includes raw diagnostics")
	}
	i := Input{RunID: "c15d003c-17e6-48d8-ab4a-96696b613d8c", Mode: "continue", ExpectedRevision: 1, Confirmed: true}
	if i.Validate() != nil {
		t.Fatal("valid repair rejected")
	}
	i.Confirmed = false
	if i.Validate() == nil {
		t.Fatal("unconfirmed mutation")
	}
}
