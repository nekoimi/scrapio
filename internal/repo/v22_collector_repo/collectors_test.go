package v22_collector_repo

import (
	"testing"

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

func TestIdempotencyKeyLimit(t *testing.T) {
	if err := validateIdempotencyKey("short-key"); err != nil {
		t.Fatal(err)
	}
	if err := validateIdempotencyKey(string(make([]rune, 129))); err == nil {
		t.Fatal("expected overlong key to fail")
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
