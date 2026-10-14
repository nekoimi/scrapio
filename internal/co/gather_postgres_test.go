package co

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"strconv"
	"testing"
	"xorm.io/xorm"
)

// Synthetic durable barrier checks, inside the existing isolated 12s fixture.
func checkO5GatherPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, versionID string) {
	t.Helper()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(q string, args ...any) { t.Helper(); _, e := engine.Exec(append([]any{q}, args...)...); must(e) }
	result := func(o *Operation) map[string]any {
		t.Helper()
		var v map[string]any
		must(json.Unmarshal([]byte(o.Result), &v))
		return v
	}
	count := func(q string, args ...any) int {
		t.Helper()
		r, e := engine.QueryString(append([]any{q}, args...)...)
		must(e)
		n, e := strconv.Atoi(r[0]["n"])
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
	batch := old.CollectorDefinition
	batch.InputFields = append([]collector.Field{}, batch.InputFields...)
	for i := range batch.InputFields {
		if batch.InputFields[i].Key == "url" {
			batch.InputFields[i].Default = json.RawMessage(`"https://fixture.example.test/aggregate"`)
		}
		if batch.InputFields[i].Key == "movie_id" {
			batch.InputFields[i].Default = json.RawMessage(`"batch"`)
		}
	}
	batch.InputFields = append(batch.InputFields, collector.Field{Key: "urls", Type: "url", Required: true, Multiple: true, EmptyAllowed: true}, collector.Field{Key: "ids", Type: "string", Required: true, Multiple: true, EmptyAllowed: true})
	must(batch.Validate())
	cid, vid, proof := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO co_collectors(id,owner_id,name,revision,definition) VALUES(?,1,'batch',1,CAST(? AS jsonb))`, cid, collector.JSON(batch))
	exec(`INSERT INTO co_operations(id,owner_id,resource_id,operation,key,fingerprint,status) VALUES(?,1,?,'proof',?,'proof','succeeded')`, proof, cid, proof)
	exec(`INSERT INTO co_collector_versions(id,owner_id,collector_id,number,revision,definition,definition_hash,input_schema_hash,output_schema_hash,interpreter_version,publish_version,requirements,evidence_id,check_id) VALUES(?,1,?,1,1,CAST(? AS jsonb),?,?,?,?,'co.publish.v1','{}',?,?)`, vid, cid, collector.JSON(batch), collector.Hash(batch), collector.Hash(batch.InputFields), collector.OutputSchemaHash(batch), collector.Interpreter, proof, proof)
	before := count("SELECT COUNT(*) AS n FROM co_records")
	run := func(name, mode, outcome, namespace string, maxRows int) string {
		t.Helper()
		d, _ := gatherFixture(mode)
		d.Source = old.Definition.Source
		d.Source.Inputs = d.Source.Inputs[:1]
		for i := range d.Stages {
			d.Stages[i].CollectorVersionID = old.Collector.ID
		}
		if mode == "batch" {
			d.Stages[3].CollectorVersionID = vid
		}
		d.Stages[3].Gather.MaxRows = maxRows
		d.Budget.MaxPending = 1
		for i := 0; i < 2; i++ {
			d.Edges[i].Mapping = map[string]collector.Binding{"url": {Kind: "field", Path: "output.comments_url"}, "movie_id": {Kind: "field", Path: "output.movie_id"}}
		}
		if mode == "join" {
			d.Edges[2].Mapping["url"] = collector.Binding{Kind: "field", Path: "output.comments_url"}
		} else {
			d.Edges[2].Mapping["urls"] = collector.Binding{Kind: "field", Path: "output.comments_url"}
		}
		o, e := CreateTask(ctx, 1, name, name+"-task", d)
		must(e)
		taskID := result(o)["id"].(string)
		o, e = PublishTask(ctx, 1, taskID, name+"-publish", 1)
		must(e)
		o, e = StartTask(ctx, 1, taskID, name+"-run", result(o)["id"].(string), namespace)
		must(e)
		id := result(o)["id"].(string)
		must(prepareRun(ctx, id))
		execute := func(c *WorkClaim, empty bool) {
			t.Helper()
			values := map[string]any{"movie_id": "m1", "title": "fixture", "rating": 9.1, "comments_url": "https://fixture.example.test/subject/m1"}
			rows := []collector.Candidate{{Identity: "m1", Valid: true, Values: values}}
			if empty {
				rows = nil
			}
			if c.StageID == "right" && outcome == "duplicate" {
				rows = append(rows, collector.Candidate{Identity: "duplicate", Valid: true, Values: values})
			}
			if c.StageID == "right" && outcome == "missing" {
				values["movie_id"] = "m2"
			}
			cp := collector.Checkpoint{Pages: 1, Records: len(rows), Done: true, EmptyVerified: empty, FinalStatus: "succeeded", Resume: "url_or_cursor", DefinitionHash: collector.Hash(c.Manifest.CollectorDefinition), InputHash: canonicalHash(c.Item.InputValues)}
			must(CommitBatch(ctx, c, rows, cp))
			must(FinishWork(ctx, c, collector.Result{Status: "succeeded"}))
		}
		firstBranch := ""
		seenSink := false
		for step := 0; step < 20; step++ {
			must(dispatchRun(ctx, id))
			must(prepareRun(ctx, id))
			c, e := ClaimWork(ctx, false)
			must(e)
			if c == nil {
				break
			}
			if c.Run.ID != id {
				t.Fatal("unexpected run")
			}
			if c.StageID == "sink" {
				seenSink = true
				if count("SELECT COUNT(*) AS n FROM co_stage_runs WHERE run_id=? AND stage_id IN ('left','right') AND status='succeeded'", id) != 2 {
					t.Fatal("barrier released early")
				}
				var input map[string]any
				must(json.Unmarshal([]byte(c.Item.InputValues), &input))
				if mode == "batch" && len(input["urls"].([]any)) != 1 {
					t.Fatal("batch wrong", input)
				}
				if count("SELECT COUNT(*) AS n FROM co_dispatches d JOIN co_dispatch_plans p ON p.id=d.plan_id WHERE d.run_id=? AND p.to_stage='sink' AND d.status='buffered'", id) < 1 {
					t.Fatal("source receipt disappeared")
				}
				if count("SELECT COUNT(*) AS n FROM co_lineage WHERE target_item_id=?", c.Item.ID) != (map[bool]int{true: 1, false: 2}[outcome == "empty"]) {
					t.Fatal("missing lineage")
				}
			}
			if c.StageID == "right" && outcome == "failed" {
				must(FinishWork(ctx, c, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_FAILURE"}}))
			} else {
				execute(c, c.StageID == "right" && outcome == "empty")
			}
			if mode == "join" && outcome == "success" && maxRows > 1 && firstBranch != "" && (c.StageID == "left" || c.StageID == "right") {
				// Fail after a group is assembled but before its child can commit.
				// The complete dispatch/seal transaction must roll back, then recover.
				exec(`CREATE FUNCTION fail_gather() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source_key LIKE 'gather:%' THEN RAISE EXCEPTION 'injected gather materialization failure'; END IF; RETURN NEW; END $$`)
				exec(`CREATE TRIGGER fail_gather BEFORE INSERT ON co_work_items FOR EACH ROW EXECUTE FUNCTION fail_gather()`)
				if e := dispatchRun(ctx, id); e == nil {
					t.Fatal("injected gather failure ignored")
				}
				if count("SELECT COUNT(*) AS n FROM co_gather_groups WHERE run_id=?", id) != 0 || count("SELECT COUNT(*) AS n FROM co_stage_runs WHERE run_id=? AND stage_id='sink' AND gather_sealed=TRUE", id) != 0 {
					t.Fatal("half-sealed transaction survived")
				}
				exec("DROP TRIGGER fail_gather ON co_work_items")
				exec("DROP FUNCTION fail_gather()")
			}
			if (c.StageID == "left" || c.StageID == "right") && firstBranch == "" {
				firstBranch = c.StageID
				must(dispatchRun(ctx, id))
				if count("SELECT COUNT(*) AS n FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND st.stage_id='sink'", id) != 0 {
					t.Fatal("first source executed barrier")
				}
				// Pause/resume after buffering: persisted receipts survive and are not redelivered.
				if outcome == "success" {
					_, e = ControlRun(ctx, 1, id, name+"-pause", "pause")
					must(e)
					_, e = ControlRun(ctx, 1, id, name+"-resume", "resume")
					must(e)
				}
			}
		}
		must(dispatchRun(ctx, id))
		must(prepareRun(ctx, id))
		must(dispatchRun(ctx, id))
		state, e := engine.QueryString("SELECT status FROM co_runs WHERE id=?", id)
		must(e)
		want := "succeeded"
		if outcome == "missing" || outcome == "duplicate" || outcome == "failed" || mode == "join" && outcome == "empty" || maxRows == 1 {
			want = "partial"
		}
		if state[0]["status"] != want {
			t.Fatal(name, state, "expected", want)
		}
		expectedSink := want == "succeeded"
		if seenSink != expectedSink {
			t.Fatal(name, "sink executed", seenSink)
		}
		// Repeated settlement/dispatch never generates duplicate groups or children.
		must(dispatchRun(ctx, id))
		if count("SELECT COUNT(*) AS n FROM co_gather_groups WHERE run_id=? AND status='delivered'", id) != map[bool]int{true: 1, false: 0}[expectedSink] {
			t.Fatal("group materialization repeated")
		}
		if count("SELECT COUNT(*) AS n FROM co_records") != before {
			t.Fatal("gather wrote business rows")
		}
		if _, e = RunPage(ctx, 2, id, "gather-groups", "", 20); e != ErrNotFound {
			t.Fatal("group owner isolation", e)
		}
		if _, e = RunDetail(ctx, 1, id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	id := run("gather-join", "join", "success", "formal", 100)
	run("gather-batch", "batch", "success", "trial", 100)
	run("gather-empty", "batch", "empty", "trial", 100)
	run("gather-missing", "join", "missing", "formal", 100)
	run("gather-duplicate", "join", "duplicate", "formal", 100)
	run("gather-limit", "join", "success", "trial", 1)
	run("gather-failed", "join", "failed", "formal", 100)
	groups, e := RunPage(ctx, 1, id, "gather-groups", "", 1)
	must(e)
	if groups == nil {
		t.Fatal("no diagnostics")
	}
	outputs, e := engine.QueryString("SELECT id FROM co_run_outputs WHERE run_id=? AND source ->> 'stage_id'='left'", id)
	must(e)
	if _, e = ReplayStage(ctx, 1, id, "gather-single-replay", ReplayRequest{StageID: "sink", EdgeID: "left_sink", OutputIDs: []string{outputs[0]["id"]}, Confirmed: true}); e == nil {
		t.Fatal("single fragment replay accepted")
	}
}
