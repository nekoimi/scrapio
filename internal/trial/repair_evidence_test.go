package trial

import (
	"context"
	"errors"
	"github.com/nekoimi/scrapio/internal/capture"
	"testing"
)

func TestD01ExtractionFailurePreservesBoundedInputWithoutOutput(t *testing.T) {
	r, fixed := jsonRunner(t)
	fixed.Content = `{"unexpected":true}`
	fixed.Hash = capture.Hash([]byte(fixed.Content))
	r.Fixed["items:list"] = fixed
	var saved *Document
	r.Emit = func(e Event, d *Document) error {
		if d != nil {
			if e.Status != "failed" {
				t.Fatal("failure document emitted as success")
			}
			saved = d
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Status != "partial" || result.Output != nil || result.Candidates != 0 || saved == nil || saved.ErrorCode == "" || saved.Content != fixed.Content || saved.ContentHash != fixed.Hash {
		t.Fatal("failed input lost or treated as output", result, saved)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	saved = nil
	r.Run(ctx)
	if saved != nil {
		t.Fatal("cancelled attempt wrote late evidence")
	}
}
func TestD01EvidenceWriteFailureStopsProcessing(t *testing.T) {
	r, fixed := jsonRunner(t)
	fixed.Content = `{"unexpected":true}`
	fixed.Hash = capture.Hash([]byte(fixed.Content))
	r.Fixed["items:list"] = fixed
	r.Emit = func(e Event, d *Document) error {
		if d != nil {
			return errors.New("LEASE_LOST")
		}
		return nil
	}
	result := r.Run(context.Background())
	if result.Reason != "LEASE_LOST" || result.Pages != 0 || result.Output != nil {
		t.Fatal("ignored evidence fencing failure", result)
	}
}
