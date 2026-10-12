package trialworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/trial"
)

// The sampling seam exposes only read operations, never navigation or back.
type samplingBrowser struct {
	gets    int
	reads   int
	state   func(int) (drission_rod.EditorSessionState, error)
	inspect func(context.Context, int, editor.Inspection) (json.RawMessage, error)
}

func (b *samplingBrowser) GetEditorSession(_ context.Context, id string) (drission_rod.EditorSessionState, error) {
	b.gets++
	return b.state(b.gets)
}
func (b *samplingBrowser) InspectEditorPage(ctx context.Context, _, _, operation string, input editor.Inspection) (json.RawMessage, error) {
	b.reads++
	if operation != "snapshot" {
		return nil, errors.New("UNEXPECTED_OPERATION")
	}
	return b.inspect(ctx, b.reads, input)
}

const listURL = "https://example.com/list"

func samplingSource() *source {
	return &source{id: "session", plan: trial.Plan{EntryType: "web"}, input: trial.Input{Origins: []string{"https://example.com"}},
		state: sampleState("receipt", listURL)}
}
func sampleState(token, url string) drission_rod.EditorSessionState {
	return drission_rod.EditorSessionState{SessionID: "session", Status: "ready", PageStateID: token, CurrentURL: url, ExpiresAt: time.Now().Add(time.Minute)}
}
func snapshotJSON(url string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"html": "<div class='item'>one</div>", "current_url": url, "base_url": url})
	return raw
}

func TestSnapshotRefreshesReceiptAndRetriesOnlyRead(t *testing.T) {
	s := samplingSource()
	b := &samplingBrowser{
		state: func(i int) (drission_rod.EditorSessionState, error) {
			return sampleState(fmt.Sprint("page-", i), listURL), nil
		},
		inspect: func(_ context.Context, i int, input editor.Inspection) (json.RawMessage, error) {
			if input.PageStateID != fmt.Sprint("page-", i) {
				t.Fatalf("inspection used old action receipt: %s", input.PageStateID)
			}
			if i == 1 {
				return nil, &drission_rod.BrowserError{Code: "STALE_PAGE_STATE"}
			}
			return snapshotJSON(listURL), nil
		},
	}
	doc, err := s.snapshot(context.Background(), b)
	if err != nil || b.gets != 2 || b.reads != 2 || s.state.PageStateID != "page-2" || doc.URL != listURL || doc.Hash != capture.Hash([]byte(doc.Content)) {
		t.Fatalf("read-only recovery failed: doc=%+v err=%v gets=%d reads=%d state=%+v", doc, err, b.gets, b.reads, s.state)
	}
}

func TestSnapshotRejectsChangedPageAndOriginBeforeReading(t *testing.T) {
	for _, tt := range []struct{ url, code string }{
		{"https://example.com/other", "RUN_PAGE_STATE_CHANGED"},
		{"https://other.com/list", "ORIGIN_OUT_OF_SCOPE"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			s := samplingSource()
			b := &samplingBrowser{state: func(int) (drission_rod.EditorSessionState, error) { return sampleState("new", tt.url), nil }}
			_, err := s.snapshot(context.Background(), b)
			if err == nil || err.Error() != tt.code || b.reads != 0 || s.state.CurrentURL != listURL {
				t.Fatalf("unexpected page adopted: err=%v reads=%d state=%+v", err, b.reads, s.state)
			}
		})
	}
}

func TestSnapshotChecksActualReturnedURL(t *testing.T) {
	s := samplingSource()
	b := &samplingBrowser{state: func(int) (drission_rod.EditorSessionState, error) { return sampleState("new", listURL), nil },
		inspect: func(context.Context, int, editor.Inspection) (json.RawMessage, error) {
			return snapshotJSON("https://example.com/other"), nil
		}}
	_, err := s.snapshot(context.Background(), b)
	if err == nil || err.Error() != "RUN_PAGE_STATE_CHANGED" || b.reads != 1 {
		t.Fatalf("snapshot of another page accepted: %v", err)
	}
}

func TestSnapshotBoundsUnstablePageAndDoesNotRetryOtherFailures(t *testing.T) {
	for _, tt := range []struct {
		code, want string
		reads      int
	}{
		{"STALE_PAGE_STATE", "PAGE_NOT_STABLE", 3},
		{"SESSION_GONE", "SESSION_GONE", 1},
		{"INVALID_RESPONSE", "INVALID_RESPONSE", 1},
	} {
		t.Run(tt.code, func(t *testing.T) {
			b := &samplingBrowser{state: func(int) (drission_rod.EditorSessionState, error) { return sampleState("new", listURL), nil },
				inspect: func(context.Context, int, editor.Inspection) (json.RawMessage, error) {
					return nil, &drission_rod.BrowserError{Code: tt.code}
				}}
			_, err := samplingSource().snapshot(context.Background(), b)
			if err == nil || err.Error() != tt.want || b.reads != tt.reads || b.gets != tt.reads {
				t.Fatalf("unexpected retries: %v reads=%d gets=%d", err, b.reads, b.gets)
			}
		})
	}
}

func TestSnapshotCancellationStopsRetryAndUsesParentBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b := &samplingBrowser{state: func(int) (drission_rod.EditorSessionState, error) { return sampleState("new", listURL), nil },
		inspect: func(ctx context.Context, _ int, _ editor.Inspection) (json.RawMessage, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Fatal("sampling exceeded parent time budget")
			}
			cancel()
			return nil, &drission_rod.BrowserError{Code: "STALE_PAGE_STATE"}
		}}
	_, err := samplingSource().snapshot(ctx, b)
	if !errors.Is(err, context.Canceled) || b.reads != 1 || b.gets != 1 {
		t.Fatalf("sampling continued after cancellation: %v reads=%d gets=%d", err, b.reads, b.gets)
	}
}

func TestSnapshotRejectsWrongSessionAndNotReadyState(t *testing.T) {
	for _, tt := range []struct{ session, status, code string }{
		{"other-session", "ready", "INVALID_RESPONSE"},
		{"session", "disconnected", "TRIAL_SESSION_NOT_READY"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			state := sampleState("new", listURL)
			state.SessionID = tt.session
			state.Status = tt.status
			b := &samplingBrowser{state: func(int) (drission_rod.EditorSessionState, error) { return state, nil }}
			_, err := samplingSource().snapshot(context.Background(), b)
			if err == nil || err.Error() != tt.code || b.reads != 0 {
				t.Fatalf("invalid session sampled: %v", err)
			}
		})
	}
}
