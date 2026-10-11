package v3

import (
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"testing"
)

func TestSessionMetadataNeverRevivesTerminalOrMismatchedState(t *testing.T) {
	state := drission_rod.EditorSessionState{SessionID: "session", PageStateID: "page", State: &editor.State{Protocol: editor.StateProtocol, PageStateID: "page"}}
	for _, status := range []string{"closed", "expired", "failed", "disconnected"} {
		row := &table.V22BrowserSession{Id: "session", PageStateId: "page", Status: status}
		attachSessionState(row, state)
		if len(sessionDTO(row).State) != 0 {
			t.Fatal("terminal state acquired live metadata")
		}
	}
	row := &table.V22BrowserSession{Id: "session", PageStateId: "new-page", Status: "ready"}
	attachSessionState(row, state)
	if len(sessionDTO(row).State) != 0 {
		t.Fatal("old state attached to new DB state")
	}
	row.PageStateId = "page"
	attachSessionState(row, state)
	if len(sessionDTO(row).State) == 0 {
		t.Fatal("matched live state lost")
	}
}
