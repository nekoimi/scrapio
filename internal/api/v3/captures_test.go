package v3

import (
	"github.com/nekoimi/scrapio/internal/db/table"
	"testing"
)

func TestCaptureDTORecovery(t *testing.T) {
	for _, tc := range []struct {
		source, status, stage string
		accessed              bool
		want                  any
	}{
		{"http", "running", "", false, nil},
		{"http", "uncertain", "recovery", false, nil},
		{"http", "uncertain", "fetch", true, true},
		{"http", "failed", "credential", false, false},
		{"offline", "succeeded", "", false, false},
	} {
		dto := captureDTO(&table.V22Capture{Source: tc.source, Status: tc.status, ErrorStage: tc.stage, NetworkAccessed: tc.accessed})
		if dto["network_accessed"] != tc.want {
			t.Fatalf("%+v: got %v", tc, dto["network_accessed"])
		}
		if _, exists := dto["request"]; exists {
			t.Fatal("internal request journal exposed")
		}
	}
}
