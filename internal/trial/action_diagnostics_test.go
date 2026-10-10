package trial

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/editor"
)

func TestC04ActionJournalUnknownAndCancellation(t *testing.T) {
	r, src := loopRunner(htmlDoc(`<div class="item">one</div><button class="next">next</button>`, "https://example.com/list"))
	r.Plan.Steps = []Step{{ID: "secret", Kind: "action", Action: editor.Command{Type: "input", Value: "private-password"}}}
	src.actionError = errors.New("ACTION_OUTCOME_UNCERTAIN")
	events := []Event{}
	r.Emit = func(e Event, _ *Document) error {
		if e.Action != nil {
			events = append(events, e)
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Reason != "ACTION_OUTCOME_UNCERTAIN" || len(src.actions) != 1 || len(events) != 2 || events[0].Status != "running" || events[1].Status != "unknown" || events[0].Action.ID != events[1].Action.ID {
		t.Fatal(result, events, src.actions)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), "private-password") {
		t.Fatal("command value leaked into event journal")
	}
	r, src = loopRunner(htmlDoc(`<div class="item">one</div>`, "https://example.com/list"))
	r.Plan.Steps = []Step{{ID: "stop", Kind: "action", Action: editor.Command{Type: "click"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Emit = func(e Event, _ *Document) error {
		if e.Action != nil && e.Status == "running" {
			cancel()
		}
		return nil
	}
	if result := r.Run(ctx); result.Status != "cancelled" || len(src.actions) != 0 {
		t.Fatal("action dispatched after cancelled pending boundary", result, src.actions)
	}
}
