package co

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

// Synthetic transactions only, inside O2's bounded disposable schema.
func checkO5Postgres(t *testing.T, ctx context.Context, engine *xorm.Engine, taskID, versionID string) {
	t.Helper()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	result := func(o *Operation) map[string]any {
		t.Helper()
		var v map[string]any
		must(json.Unmarshal([]byte(o.Result), &v))
		return v
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
	d := old.Definition
	st := d.Stages[0]
	st.InputPolicy = "merge_identical"
	d.Stages = []TaskStage{st}
	a, b := d.Source.Inputs[0], d.Source.Inputs[1]
	d.Source.Inputs = []map[string]any{a, a, b}
	d.Budget.MaxWorkItems, d.Budget.MaxPending = 3, 1
	// Reuse the same collector in another stage. It must have its own work item.
	o, e := CreateDataTable(ctx, 1, "o5-child", "o5-child-table", old.Schema)
	must(e)
	table := result(o)
	child := st
	child.ID, child.Name = "child", "独立子阶段"
	child.Save.TableID = table["id"].(string)
	child.Save.SchemaHash = table["schema_hash"].(string)
	d.Stages = append(d.Stages, child)
	d.Edges = []TaskEdge{{ID: "same_target", From: st.ID, To: child.ID, Port: "records", Emit: "valid", Mapping: map[string]collector.Binding{}}}
	for k, value := range a {
		d.Edges[0].Mapping[k] = collector.Binding{Kind: "constant", Value: json.RawMessage(collector.JSON(value))}
	}
	o, e = CreateTask(ctx, 1, "o5 merge", "o5-task", d)
	must(e)
	taskID = result(o)["id"].(string)
	o, e = PublishTask(ctx, 1, taskID, "o5-publish", 1)
	must(e)
	versionID = result(o)["id"].(string)
	o, e = StartTask(ctx, 1, taskID, "o5-run", versionID, "formal")
	must(e)
	runID := result(o)["id"].(string)
	must(prepareRun(ctx, runID))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", runID) != 1 || count("SELECT preparing_cursor AS n FROM co_runs WHERE id=?", runID) != 2 {
		t.Fatal("root duplicate must merge even with a full one-slot queue")
	}
	execute := func(c *WorkClaim) {
		t.Helper()
		var input map[string]any
		must(json.Unmarshal([]byte(c.Item.InputValues), &input))
		values := map[string]any{"movie_id": input["movie_id"], "title": "fixture", "rating": 9.2, "comments_url": "https://fixture.example.test/comments"}
		cp := collector.Checkpoint{Pages: 1, Records: 1, Resume: "url_or_cursor", InputHash: canonicalHash(c.Item.InputValues), DefinitionHash: collector.Hash(c.Manifest.CollectorDefinition), Done: true, FinalStatus: "succeeded"}
		must(CommitBatch(ctx, c, []collector.Candidate{{Identity: input["movie_id"].(string), Values: values, Valid: true}}, cp))
		must(FinishWork(ctx, c, collector.Result{Status: "succeeded"}))
	}
	claim := func(stage string) *WorkClaim {
		t.Helper()
		c, e := ClaimWork(ctx, false)
		must(e)
		if c == nil || c.Run.ID != runID || c.StageID != stage {
			t.Fatal("missing expected merge fixture work", stage, c)
		}
		return c
	}
	root := claim(st.ID)
	execute(root)
	must(dispatchRun(ctx, runID))
	target := claim(child.ID)
	if root.Item.ID == target.Item.ID {
		t.Fatal("different stages merged")
	}
	execute(target)
	must(prepareRun(ctx, runID))
	execute(claim(st.ID))
	// The canonical child is already succeeded and the work-item budget is full.
	// A late duplicate still attaches its provenance; it never emits old output again.
	must(dispatchRun(ctx, runID))
	must(dispatchRun(ctx, runID))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", runID) != 3 || count("SELECT COUNT(*) AS n FROM co_item_sources WHERE run_id=?", runID) != 5 || count("SELECT COUNT(*) AS n FROM co_dispatches WHERE run_id=? AND status='merged'", runID) != 1 || count("SELECT COUNT(*) AS n FROM co_run_outputs WHERE run_id=?", runID) != 3 {
		t.Fatal("merge lost provenance or duplicated work/output/dispatch")
	}
	view, e := RunDetail(ctx, 1, runID)
	must(e)
	raw := view.(map[string]any)["run"].(json.RawMessage)
	var r struct{ Status string }
	must(json.Unmarshal(raw, &r))
	if r.Status != "succeeded" {
		t.Fatal("merged dispatch became incomplete coverage", r.Status)
	}
	detail, e := ItemDetail(ctx, 1, runID, target.Item.ID)
	must(e)
	info := detail.(map[string]any)
	if len(info["sources"].([]json.RawMessage)) != 2 || len(info["lineage"].([]json.RawMessage)) != 2 {
		t.Fatal("late source or multi-source ancestry missing")
	}
	if _, e = ItemDetail(ctx, 2, runID, target.Item.ID); e != ErrNotFound {
		t.Fatal("merged sources owner isolation", e)
	}
	if _, e = ItemDetail(ctx, 1, runID, target.Item.ID, "invalid"); e == nil {
		t.Fatal("invalid source cursor accepted")
	}
	// A new trial is an independent merge domain; it cannot reuse formal outcomes.
	o, e = StartTask(ctx, 1, taskID, "o5-trial", versionID, "trial")
	must(e)
	trialID := result(o)["id"].(string)
	must(prepareRun(ctx, trialID))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", trialID) != 1 {
		t.Fatal("new trial reused formal work")
	}
	_, e = ControlRun(ctx, 1, trialID, "o5-trial-cancel", "cancel")
	must(e)
	// Default compatibility: equal inputs create separate work and failed-only replay
	// of a merged canonical item creates exactly one new root with original item refs.
	d.Stages = d.Stages[:1]
	d.Edges = []TaskEdge{}
	d.Source.Inputs = []map[string]any{a, a}
	d.Stages[0].InputPolicy = ""
	o, e = CreateTask(ctx, 1, "o5 keep", "o5-keep-task", d)
	must(e)
	keepTask := result(o)["id"].(string)
	o, e = PublishTask(ctx, 1, keepTask, "o5-keep-version", 1)
	must(e)
	o, e = StartTask(ctx, 1, keepTask, "o5-keep-run", result(o)["id"].(string), "formal")
	must(e)
	keepID := result(o)["id"].(string)
	must(prepareRun(ctx, keepID))
	c, e := ClaimWork(ctx, false)
	must(e)
	if c == nil || c.Run.ID != keepID {
		t.Fatal("missing keep-all input")
	}
	must(FinishWork(ctx, c, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_FAILURE", Message: "terminal synthetic failure"}}))
	must(prepareRun(ctx, keepID))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", keepID) != 2 {
		t.Fatal("old policy merged failed input")
	}
	_, e = ControlRun(ctx, 1, keepID, "o5-keep-cancel", "cancel")
	must(e)
	d.Stages[0].InputPolicy = "merge_identical"
	o, e = CreateTask(ctx, 1, "o5 failure", "o5-failure-task", d)
	must(e)
	failedTask := result(o)["id"].(string)
	o, e = PublishTask(ctx, 1, failedTask, "o5-failure-version", 1)
	must(e)
	o, e = StartTask(ctx, 1, failedTask, "o5-failure-run", result(o)["id"].(string), "formal")
	must(e)
	failedID := result(o)["id"].(string)
	must(prepareRun(ctx, failedID))
	c, e = ClaimWork(ctx, false)
	must(e)
	if c == nil || c.Run.ID != failedID {
		t.Fatal("missing shared-failure item")
	}
	must(FinishWork(ctx, c, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_FAILURE", Message: "shared failure"}}))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", failedID) != 1 || count("SELECT COUNT(*) AS n FROM co_item_sources WHERE run_id=?", failedID) != 2 {
		t.Fatal("shared failure provenance")
	}
	o, e = RetryFailed(ctx, 1, failedID, "o5-retry", true)
	must(e)
	retryID := result(o)["id"].(string)
	must(prepareRun(ctx, retryID))
	if count("SELECT COUNT(*) AS n FROM co_work_items WHERE run_id=?", retryID) != 1 {
		t.Fatal("shared failure replay expanded duplicate sources")
	}
	_, e = ControlRun(ctx, 1, retryID, "o5-retry-cancel", "cancel")
	must(e)
}
