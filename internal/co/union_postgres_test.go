package co

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

// Synthetic diamond DAG only, within O2's disposable schema and deadline.
func checkO5UnionPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, versionID string) {
	t.Helper()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	result := func(o *Operation) map[string]any {
		t.Helper()
		var r map[string]any
		must(json.Unmarshal([]byte(o.Result), &r))
		return r
	}
	count := func(q string, args ...any) int {
		t.Helper()
		rows, e := engine.QueryString(append([]any{q}, args...)...)
		must(e)
		n, e := strconv.Atoi(rows[0]["n"])
		must(e)
		return n
	}
	v := new(TaskVersion)
	s := session(ctx)
	_, e := s.ID(versionID).Get(v)
	s.Close()
	must(e)
	var old TaskManifest
	must(collector.Strict([]byte(v.Manifest), &old))
	d := unionFixture()
	d.Source = old.Definition.Source
	d.Source.Inputs = d.Source.Inputs[:1]
	for i := range d.Stages {
		d.Stages[i].CollectorVersionID = old.Collector.ID
	}
	for i := range d.Edges {
		d.Edges[i].Mapping = map[string]collector.Binding{"url": {Kind: "field", Path: "output.comments_url"}, "movie_id": {Kind: "field", Path: "output.movie_id"}}
	}
	d.Budget.MaxPending = 2
	d.Stages[3].InputPolicy = "merge_identical"
	formalBefore := count("SELECT COUNT(*) AS n FROM co_records")
	run := func(key, policy, namespace, rightOutcome string, maxItems int) (string, string) {
		t.Helper()
		local := d
		local.Stages = append([]TaskStage{}, d.Stages...)
		local.Stages[3].InputPolicy = policy
		local.Budget.MaxWorkItems = maxItems
		o, e := CreateTask(ctx, 1, key, key+"-task", local)
		must(e)
		taskID := result(o)["id"].(string)
		o, e = PublishTask(ctx, 1, taskID, key+"-publish", 1)
		must(e)
		version := result(o)["id"].(string)
		o, e = StartTask(ctx, 1, taskID, key+"-run", version, namespace)
		must(e)
		id := result(o)["id"].(string)
		must(prepareRun(ctx, id))
		claim := func() *WorkClaim {
			t.Helper()
			c, e := ClaimWork(ctx, false)
			must(e)
			if c == nil || c.Run.ID != id {
				t.Fatal("missing union work", key, c)
			}
			return c
		}
		execute := func(c *WorkClaim) {
			t.Helper()
			values := map[string]any{"movie_id": "union-movie", "title": "fixture", "rating": 9.1, "comments_url": "https://fixture.example.test/subject/union"}
			cp := collector.Checkpoint{Pages: 1, Records: 1, Done: true, FinalStatus: "succeeded", Resume: "url_or_cursor", DefinitionHash: collector.Hash(c.Manifest.CollectorDefinition), InputHash: canonicalHash(c.Item.InputValues)}
			must(CommitBatch(ctx, c, []collector.Candidate{{Identity: "union-id", Valid: true, Values: values}}, cp))
			must(FinishWork(ctx, c, collector.Result{Status: "succeeded"}))
		}
		root := claim()
		if root.StageID != "root" {
			t.Fatal("wrong root")
		}
		execute(root)
		if count("SELECT COUNT(*) AS n FROM co_dispatch_plans WHERE run_id=? AND status='pending'", id) != 2 {
			t.Fatal("root outbox not frozen")
		}
		must(dispatchRun(ctx, id))
		// Hold both branches as real running claims. Finish the first and allow
		// its shared descendant to run before the second parent has produced.
		a, b := claim(), claim()
		if a.StageID == b.StageID || (a.StageID != "left" && a.StageID != "right") || (b.StageID != "left" && b.StageID != "right") {
			t.Fatal("wrong branch claims")
		}
		left, right := a, b
		if left.StageID != "left" {
			left, right = right, left
		}
		execute(left)
		must(dispatchRun(ctx, id))
		if count("SELECT COUNT(*) AS n FROM co_stage_runs WHERE run_id=? AND stage_id='sink' AND input_closed=FALSE", id) != 1 {
			t.Fatal("union closed on first branch")
		}
		sink := claim()
		if sink.StageID != "sink" {
			t.Fatal("streaming union did not execute before all parents")
		}
		execute(sink)
		if count("SELECT COUNT(*) AS n FROM co_stage_runs WHERE run_id=? AND stage_id='sink' AND status NOT IN ('succeeded','partial','failed','limited','cancelled') AND input_closed=FALSE", id) != 1 {
			t.Fatal("completed item closed remaining source")
		}
		if rightOutcome == "failed" {
			must(FinishWork(ctx, right, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_FAILURE", Message: "second source failed"}}))
		} else if rightOutcome == "empty" {
			cp := collector.Checkpoint{Pages: 1, Done: true, EmptyVerified: true, FinalStatus: "succeeded", Resume: "url_or_cursor", DefinitionHash: collector.Hash(right.Manifest.CollectorDefinition), InputHash: canonicalHash(right.Item.InputValues)}
			must(CommitBatch(ctx, right, nil, cp))
			must(FinishWork(ctx, right, collector.Result{Status: "succeeded"}))
		} else {
			execute(right)
		}
		for i := 0; i < 12; i++ {
			must(dispatchRun(ctx, id))
			must(prepareRun(ctx, id))
			c, e := ClaimWork(ctx, false)
			must(e)
			if c == nil {
				break
			}
			if c.Run.ID != id {
				t.Fatal("wrong active run")
			}
			execute(c)
		}
		must(dispatchRun(ctx, id))
		must(prepareRun(ctx, id))
		rd, e := RunDetail(ctx, 1, id)
		must(e)
		raw := collector.JSON(rd)
		if !strings.Contains(raw, `"upstreams"`) || !strings.Contains(raw, `"edge_id":"right_sink"`) {
			t.Fatal("union source status missing", raw)
		}
		state, e := engine.QueryString("SELECT status FROM co_runs WHERE id=?", id)
		must(e)
		want := "succeeded"
		if rightOutcome == "failed" {
			want = "partial"
		}
		if maxItems == 4 {
			want = "limited"
		}
		if state[0]["status"] != want {
			t.Fatal("union final status", key, state, "expected", want)
		}
		expected := 1
		if policy == "keep_all" && rightOutcome == "succeeded" {
			expected = 2
		}
		if count("SELECT COUNT(*) AS n FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND st.stage_id='sink'", id) != expected {
			t.Fatal("union input policy broken")
		}
		if policy == "merge_identical" && rightOutcome == "succeeded" {
			if count("SELECT COUNT(*) AS n FROM co_lineage l JOIN co_work_items w ON w.id=l.target_item_id JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND st.stage_id='sink'", id) != 2 || count("SELECT COUNT(*) AS n FROM co_dispatches WHERE run_id=? AND edge_id='right_sink' AND status='merged'", id) != 1 {
				t.Fatal("late source not retained")
			}
		}
		if count("SELECT COUNT(*) AS n FROM co_records") != formalBefore {
			t.Fatal("pass-only union wrote business records")
		}
		return id, version
	}
	id, _ := run("union-merge", "merge_identical", "formal", "succeeded", 100)
	outputs, e := engine.QueryString("SELECT id FROM co_run_outputs WHERE run_id=? AND source ->> 'stage_id'='left'", id)
	must(e)
	request := ReplayRequest{StageID: "sink", OutputIDs: []string{outputs[0]["id"]}, Confirmed: true}
	if _, e := ReplayStage(ctx, 1, id, "union-ambiguous-replay", request); e == nil || collector.AsError(e).Location["field"] != "edge_id" {
		t.Fatal("ambiguous multi-source replay accepted", e)
	}
	request.EdgeID = "right_sink"
	if _, e := ReplayStage(ctx, 1, id, "union-wrong-edge", request); e == nil || collector.AsError(e).Code != "OUTPUT_EVIDENCE_MISSING" {
		t.Fatal("replay ignored selected edge", e)
	}
	request.EdgeID = "left_sink"
	o, e := ReplayStage(ctx, 1, id, "union-explicit-replay", request)
	must(e)
	replayID := result(o)["id"].(string)
	r, e := engine.QueryString("SELECT manifest -> 'definition' -> 'stages' AS stages,retry_scope::text AS scope FROM co_runs WHERE id=?", replayID)
	must(e)
	if !strings.Contains(r[0]["scope"], "not_executed_edge:right_sink") || strings.Contains(r[0]["stages"], `"input_mode": "union"`) {
		t.Fatal("replay boundary missing", r)
	}
	_, e = ControlRun(ctx, 1, replayID, "union-replay-cancel", "cancel")
	must(e)
	run("union-keep", "keep_all", "formal", "succeeded", 100)
	run("union-empty", "merge_identical", "formal", "empty", 100)
	run("union-failed", "merge_identical", "formal", "failed", 100)
	run("union-budget", "merge_identical", "trial", "succeeded", 4)
}
