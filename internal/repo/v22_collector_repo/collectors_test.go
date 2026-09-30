package v22_collector_repo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/db/table"
)

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
		want bool
	}{
		{"web url", CreateInput{EntryURL: "https://example.org/list", EntryType: "web"}, true},
		{"json url", CreateInput{EntryURL: "http://example.org/api", EntryType: "json"}, true},
		{"relative url", CreateInput{EntryURL: "/list", EntryType: "web"}, false},
		{"unsupported scheme", CreateInput{EntryURL: "file:///tmp/data", EntryType: "json"}, false},
		{"unsupported type", CreateInput{EntryURL: "https://example.org", EntryType: "xml"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateCreate(tt.in) == nil
			if got != tt.want {
				t.Fatalf("valid = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefinitionEntryURLValidation(t *testing.T) {
	valid := json.RawMessage(`{"entry_url":"https://example.org/new","steps":[]}`)
	var definition map[string]any
	if err := json.Unmarshal(valid, &definition); err != nil {
		t.Fatal(err)
	}
	url, ok := definition["entry_url"].(string)
	if !ok || url != "https://example.org/new" {
		t.Fatalf("entry_url = %#v", definition["entry_url"])
	}
	for _, raw := range []string{`{"entry_url":false}`, `{"entry_url":"file:///tmp/a"}`, `{"entry_url":"/relative"}`} {
		var current map[string]any
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			t.Fatal(err)
		}
		candidate, isString := current["entry_url"].(string)
		if isString && validateCreate(CreateInput{EntryURL: candidate, EntryType: "web"}) == nil {
			t.Errorf("expected invalid entry_url: %s", raw)
		}
	}
}

func TestIdempotencyKeyLimit(t *testing.T) {
	if err := validateIdempotencyKey("short-key"); err != nil {
		t.Fatal(err)
	}
	if err := validateIdempotencyKey(string(make([]rune, 129))); err == nil {
		t.Fatal("expected overlong key to fail")
	}
}

func TestValidateDefinition(t *testing.T) {
	valid := json.RawMessage(`{"definition_version":1,"entry_url":"https://example.org/list","steps":[]}`)
	if result := validateDefinition(valid, "https://example.org/list"); !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("valid definition result = %+v", result)
	}
	tests := []struct {
		name     string
		raw      string
		entryURL string
	}{
		{"version", `{"definition_version":2,"entry_url":"https://example.org/list","steps":[]}`, "https://example.org/list"},
		{"entry mismatch", `{"definition_version":1,"entry_url":"https://example.org/other","steps":[]}`, "https://example.org/list"},
		{"steps missing", `{"definition_version":1,"entry_url":"https://example.org/list"}`, "https://example.org/list"},
		{"invalid url", `{"definition_version":1,"entry_url":"/list","steps":[]}`, "/list"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validateDefinition(json.RawMessage(test.raw), test.entryURL)
			if result.Valid || len(result.Errors) == 0 {
				t.Fatalf("expected validation error, got %+v", result)
			}
		})
	}
}

func TestValidationStateExpiresWithRevision(t *testing.T) {
	summary, err := json.Marshal(ValidationResult{Valid: true, Errors: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	row := &table.V22Collector{Revision: 3, ValidationSummary: string(summary)}
	if status, _ := validationState(row); status != "not_validated" {
		t.Fatalf("status without validation = %q", status)
	}
	row.ValidatedRevision = 3
	if status, _ := validationState(row); status != "valid" {
		t.Fatalf("status at validated revision = %q", status)
	}
	row.Revision++
	if status, _ := validationState(row); status != "stale" {
		t.Fatalf("status after revision change = %q", status)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	row := table.V22Collector{Id: 42, UpdatedAt: time.Date(2026, 9, 30, 10, 11, 12, 123456789, time.FixedZone("test", 8*60*60))}
	cursor := encodeCursor(row)
	updatedAt, id, err := decodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if id != row.Id || !updatedAt.Equal(row.UpdatedAt) {
		t.Fatalf("cursor decoded to (%s, %d), want (%s, %d)", updatedAt, id, row.UpdatedAt, row.Id)
	}
	if _, _, err := decodeCursor("not-a-cursor"); err == nil {
		t.Fatal("expected malformed cursor to fail")
	}
}

func TestCollectorDTOUsesStringIDAndPublicFieldsOnly(t *testing.T) {
	dto := ToDTO(&table.V22Collector{Id: 9007199254740993, OwnerId: 8, CreatedBy: 8, UpdatedBy: 8, Definition: `{"steps":[]}`})
	if dto.ID != "9007199254740993" {
		t.Fatalf("id = %q", dto.ID)
	}
	if string(dto.Definition) != `{"steps":[]}` {
		t.Fatalf("definition = %s", dto.Definition)
	}
}
