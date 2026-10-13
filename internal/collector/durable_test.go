package collector

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCOO2PageCommitBeforeNextFetchAndResume(t *testing.T) {
	b := baseline(t)
	d := b.Collectors[0].Definition
	d.Fetch.Kind = "http_html"
	d.Fetch.Method = "GET"
	input := b.Samples[0].Input
	raw, e := os.ReadFile("../../docs/项目文档v2.2/编排O0/fixtures/list.html")
	if e != nil {
		t.Fatal(e)
	}
	first := Document{URL: input["url"].(string), Format: "html", Content: string(raw)}
	// A failed batch must prevent access to page 2 and must not claim a durable checkpoint.
	source := &fakeSource{docs: []Document{first}}
	commits := 0
	r := ExecuteDurable(context.Background(), d, input, "live", source, DurableOptions{Commit: func(context.Context, []Candidate, Checkpoint) error {
		commits++
		return Fault("SQL_FAILED", "save", "injected")
	}})
	if source.opened != 1 || commits != 1 || r.Error == nil || r.Error.Code != "SQL_FAILED" {
		t.Fatal(r, source.opened, commits)
	}
	var checkpoint Checkpoint
	source = &fakeSource{docs: []Document{first}}
	r = ExecuteDurable(context.Background(), d, input, "live", source, DurableOptions{Commit: func(_ context.Context, rows []Candidate, cp Checkpoint) error {
		if len(rows) != 2 || cp.Done || cp.Pages != 1 {
			t.Fatalf("unexpected page batch: %+v", cp)
		}
		checkpoint = cp
		return nil
	}})
	if checkpoint.NextURL == "" || checkpoint.Records != 2 || r.Error == nil {
		t.Fatal(checkpoint, r)
	}
	next := Document{URL: checkpoint.NextURL, Format: "html", Content: strings.Replace(strings.Replace(first.Content, "m001", "m003", -1), "m002", "m004", -1)}
	next.Content = strings.Replace(next.Content, `class="next"`, `class="finished"`, 1)
	source = &fakeSource{docs: []Document{next}}
	commits = 0
	r = ExecuteDurable(context.Background(), d, input, "live", source, DurableOptions{Checkpoint: checkpoint, Commit: func(_ context.Context, rows []Candidate, cp Checkpoint) error {
		commits++
		if len(rows) != 2 || cp.Pages != 2 || !cp.Done || cp.Records != 4 {
			t.Fatal(rows, cp)
		}
		checkpoint = cp
		return nil
	}})
	if r.Status != "succeeded" || source.request.URL != next.URL || source.opened != 1 || commits != 1 || len(r.Candidates) != 0 {
		t.Fatal(r, source.request, commits)
	}
	source = &fakeSource{}
	r = ExecuteDurable(context.Background(), d, input, "live", source, DurableOptions{Checkpoint: checkpoint, Commit: func(context.Context, []Candidate, Checkpoint) error {
		t.Fatal("completed checkpoint reexecuted")
		return nil
	}})
	if r.Status != "succeeded" || source.opened != 0 {
		t.Fatal(r, source.opened)
	}
	checkpoint.InputHash = "other"
	r = ExecuteDurable(context.Background(), d, input, "live", source, DurableOptions{Checkpoint: checkpoint})
	if r.Reason != "CHECKPOINT_CONFLICT" {
		t.Fatal(r)
	}
}
