package co

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xorm.io/xorm"
)

// Called inside O2's opt-in disposable schema; bounded, with synthetic documents.
func checkO3Postgres(t *testing.T, ctx context.Context, root string, engine *xorm.Engine) {
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
	count := func(q string, args ...any) int64 {
		t.Helper()
		s := engine.NewSession()
		defer s.Close()
		n, e := s.SQL(q, args...).Count(new(WorkItem))
		must(e)
		return n
	}
	var fixture struct {
		Collectors []struct {
			ID         string               `json:"id"`
			Definition collector.Definition `json:"definition"`
		} `json:"collectors"`
	}
	data, e := os.ReadFile(filepath.Join(root, "docs/项目文档v2.2/编排O0/fixtures/baseline.json"))
	must(e)
	must(json.Unmarshal(data, &fixture))
	d := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "ranking", Source: ManualSource{Kind: "manual", Inputs: []map[string]any{{"url": "https://fixture.example.test/top250"}}}, OverlapPolicy: "skip", Budget: TaskBudget{MaxWorkItems: 50, MaxOutputs: 100, MaxPending: 1, BrowserSlots: 1, MaxDurationMS: 60000}}
	documents := map[string][]collector.Document{}
	for _, f := range fixture.Collectors {
		cd := f.Definition
		cd.Fetch.Kind = "http_html"
		cd.Fetch.Method = "GET"
		cd.Pagination.Kind = "none"
		cd.Pagination.EndCondition = "single_page"
		cd.Pagination.Next = nil
		cd.Pagination.MaxPages = 1
		must(cd.Validate())
		cid, vid, proof := uuid.NewString(), uuid.NewString(), uuid.NewString()
		exec(`INSERT INTO co_collectors(id,owner_id,name,revision,definition) VALUES(?,1,?,1,CAST(? AS jsonb))`, cid, f.ID, collector.JSON(cd))
		exec(`INSERT INTO co_operations(id,owner_id,resource_id,operation,key,fingerprint,status) VALUES(?,1,?,'proof',?,'proof','succeeded')`, proof, cid, proof)
		exec(`INSERT INTO co_collector_versions(id,owner_id,collector_id,number,revision,definition,definition_hash,input_schema_hash,output_schema_hash,interpreter_version,publish_version,requirements,evidence_id,check_id) VALUES(?,1,?,1,1,CAST(? AS jsonb),?,?,?,?,'co.publish.v1','{}',?,?)`, vid, cid, collector.JSON(cd), collector.Hash(cd), collector.Hash(cd.InputFields), collector.OutputSchemaHash(cd), collector.Interpreter, proof, proof)
		unique := cd.Output.IdentityFields
		if f.ID == "comment" {
			unique = []string{"movie_id", "comment_id"}
		}
		o, e := CreateDataTable(ctx, 1, "o3-"+f.ID, "o3-table-"+f.ID, TableSchema{Fields: cd.Output.Fields, UniqueKey: unique})
		must(e)
		table := result(o)
		b := SaveBinding{TableID: table["id"].(string), SchemaVersion: 1, SchemaHash: table["schema_hash"].(string), UniqueKey: unique, UpdatePolicy: "merge_present", NullPolicy: "keep", Mapping: map[string]collector.Binding{}}
		for _, field := range cd.Output.Fields {
			b.Mapping[field.Key] = collector.Binding{Kind: "field", Path: "output." + field.Key}
		}
		d.Stages = append(d.Stages, TaskStage{ID: f.ID, Name: f.ID, CollectorVersionID: vid, Save: b, OnError: "continue"})
		name := "list.html"
		urls := []string{"https://fixture.example.test/top250"}
		if f.ID == "movie" {
			name = "detail.html"
			urls = []string{"https://fixture.example.test/subject/m001", "https://fixture.example.test/subject/m002"}
		}
		if f.ID == "comment" {
			name = "comments.html"
			urls = []string{"https://fixture.example.test/subject/m001/comments", "https://fixture.example.test/subject/m002/comments"}
		}
		html, e := os.ReadFile(filepath.Join(root, "docs/项目文档v2.2/编排O0/fixtures", name))
		must(e)
		for _, u := range urls {
			body := string(html)
			if strings.Contains(u, "m002") {
				body = strings.ReplaceAll(body, "m001", "m002")
			}
			documents[f.ID] = append(documents[f.ID], collector.Document{URL: u, Format: "html", Content: body})
		}
	}
	// Reuse the same collector on a branch, with a distinct table.
	branch := d.Stages[1]
	branch.ID = "movie_branch"
	branch.Name = "电影分支"
	var bd collector.Definition
	s := engine.NewSession()
	_, bd, e = collectorVersion(s, 1, branch.CollectorVersionID)
	s.Close()
	must(e)
	o, e := CreateDataTable(ctx, 1, "o3-branch", "o3-table-branch", TableSchema{Fields: bd.Output.Fields, UniqueKey: bd.Output.IdentityFields})
	must(e)
	table := result(o)
	branch.Save.TableID = table["id"].(string)
	branch.Save.SchemaHash = table["schema_hash"].(string)
	d.Stages = append(d.Stages, branch)
	documents[branch.ID] = documents["movie"]
	edge := func(id, from, to, urlField string) TaskEdge {
		return TaskEdge{ID: id, From: from, To: to, Port: "records", Emit: "valid", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output." + urlField}, "movie_id": {Kind: "field", Path: "output.movie_id"}}}
	}
	d.Edges = []TaskEdge{edge("ranking_movie", "ranking", "movie", "detail_url"), edge("movie_comment", "movie", "comment", "comments_url"), edge("ranking_branch", "ranking", "movie_branch", "detail_url")}
	o, e = CreateTask(ctx, 1, "o3 graph", "o3-create", d)
	must(e)
	taskID := result(o)["id"].(string)
	_, e = TaskList(ctx, 1, "", 20)
	must(e)
	o, e = PublishTask(ctx, 1, taskID, "o3-publish", 1)
	must(e)
	versionID := result(o)["id"].(string)
	totalRecords := count("SELECT COUNT(*) FROM co_records")
	execute := func(c *WorkClaim, fail bool) {
		t.Helper()
		if fail && c.StageID == "movie" && strings.Contains(c.Item.InputValues, "m002") {
			must(FinishWork(ctx, c, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_FAILURE", Message: "one isolated item"}}))
			return
		}
		input := map[string]any{}
		var cp collector.Checkpoint
		must(json.Unmarshal([]byte(c.Item.InputValues), &input))
		must(json.Unmarshal([]byte(c.Item.Checkpoint), &cp))
		source := &Source{Mode: "offline", Definition: c.Manifest.CollectorDefinition, Fixed: documents[c.StageID]}
		r := collector.ExecuteDurable(ctx, c.Manifest.CollectorDefinition, input, "offline", source, collector.DurableOptions{Checkpoint: cp, Commit: func(ctx context.Context, rows []collector.Candidate, cp collector.Checkpoint) error {
			return CommitBatch(ctx, c, rows, cp)
		}})
		if r.Error != nil {
			t.Fatal("fixture execute", c.StageID, r.Error)
		}
		must(FinishWork(ctx, c, r))
	}
	run := func(key, namespace string, fail bool) string {
		t.Helper()
		o, e := StartTask(ctx, 1, taskID, key, versionID, namespace)
		must(e)
		id := result(o)["id"].(string)
		must(PrepareRuns(ctx))
		c, e := ClaimWork(ctx, false)
		must(e)
		if c == nil || c.Run.ID != id {
			t.Fatal("root claim missing")
		}
		execute(c, false)
		rd, e := RunDetail(ctx, 1, id)
		must(e)
		if strings.Contains(collector.JSON(rd), `"status":"succeeded","task_version`) {
			t.Fatal("closed before dispatch")
		}
		if count("SELECT COUNT(*) FROM co_dispatch_plans WHERE run_id=? AND status='pending'", id) == 0 {
			t.Fatal("outbox missing after commit")
		}
		// A changed business-table current value must not replace frozen outputs.
		if key == "o3-formal" {
			exec(`UPDATE co_records SET values_json=jsonb_set(values_json,'{detail_url}',CAST('"https://fixture.example.test/WRONG"' AS jsonb)) WHERE table_id=?`, d.Stages[0].Save.TableID)
		}
		for i := 0; i < 35; i++ {
			must(DispatchRuns(ctx))
			must(PrepareRuns(ctx))
			if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=? AND status IN ('queued','running')", id) > 1 {
				t.Fatal("queue capacity exceeded")
			}
			c, e = ClaimWork(ctx, false)
			must(e)
			if c != nil {
				if c.Run.ID != id {
					t.Fatal("wrong run")
				}
				execute(c, fail)
			}
			s := engine.NewSession()
			r, e := getRun(s, 1, id, false)
			s.Close()
			must(e)
			if terminal(r.Status) {
				if fail && r.Status != "partial" {
					t.Fatal("failure hidden", r.Status)
				}
				if !fail && r.Status != "succeeded" {
					t.Fatal("graph not complete", r.Status)
				}
				break
			}
			if i == 34 {
				t.Fatal("graph did not close")
			}
		}
		n := count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", id)
		must(DispatchRuns(ctx))
		must(DispatchRuns(ctx))
		if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", id) != n {
			t.Fatal("duplicate dispatch")
		}
		if n != 7 && !fail {
			t.Fatal("branch items", n)
		}
		if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=? AND input_values::text LIKE '%WRONG%'", id) != 0 {
			t.Fatal("dispatcher read current table rather than output")
		}
		if key == "o3-formal" {
			exec(`UPDATE co_records r SET values_json=o.values_json FROM co_run_outputs o JOIN co_observations ob ON ob.id=o.observation_id WHERE ob.record_id=r.id AND o.run_id=? AND r.table_id=?`, id, d.Stages[0].Save.TableID)
		}
		var item WorkItem
		s := engine.NewSession()
		ok, e := s.SQL("SELECT w.* FROM co_work_items w JOIN co_stage_runs st ON st.id=w.stage_run_id WHERE w.run_id=? AND st.stage_id='comment' LIMIT 1", id).Get(&item)
		s.Close()
		must(e)
		if !ok {
			t.Fatal("no comment lineage")
		}
		detail, e := ItemDetail(ctx, 1, id, item.ID)
		must(e)
		if len(detail.(map[string]any)["lineage"].([]json.RawMessage)) != 2 {
			t.Fatal("lineage not two hops", detail)
		}
		return id
	}
	formal := run("o3-formal", "formal", false)
	if count("SELECT COUNT(*) FROM co_records")-totalRecords != 10 {
		t.Fatal("stage tables not saved")
	}
	// unchanged outputs must still drive the entire graph.
	run("o3-unchanged", "formal", false)
	if count("SELECT COUNT(*) FROM co_records")-totalRecords != 10 {
		t.Fatal("unchanged duplicated rows")
	}
	before := count("SELECT COUNT(*) FROM co_record_revisions")
	run("o3-trial", "trial", true)
	if count("SELECT COUNT(*) FROM co_records")-totalRecords != 10 || count("SELECT COUNT(*) FROM co_record_revisions") != before {
		t.Fatal("trial wrote formal rows")
	}
	if _, e = RunDetail(ctx, 2, formal); e != ErrNotFound {
		t.Fatal("foreign run visible", e)
	}
	// Cancellation between output commit and dispatch is fenced and retains output.
	o, e = StartTask(ctx, 1, taskID, "o3-cancel", versionID, "formal")
	must(e)
	cancelID := result(o)["id"].(string)
	must(PrepareRuns(ctx))
	c, e := ClaimWork(ctx, false)
	must(e)
	execute(c, false)
	_, e = ControlRun(ctx, 1, cancelID, "o3-cancel-control", "cancel")
	must(e)
	must(DispatchRuns(ctx))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", cancelID) != 1 || count("SELECT COUNT(*) FROM co_dispatch_plans WHERE run_id=? AND status='pending'", cancelID) != 0 {
		t.Fatal("dispatch after cancel")
	}
	// The public offline-trial contract uses the HTTP pool even for browser stages.
	o, e = StartTask(ctx, 1, taskID, "o3-offline", versionID, "trial", TrialOptions{Mode: "offline", Documents: documents})
	must(e)
	offlineID := result(o)["id"].(string)
	must(PrepareRuns(ctx))
	c, e = ClaimWork(ctx, false)
	must(e)
	if c == nil || c.Run.ID != offlineID || c.Manifest.Trial.Mode != "offline" {
		t.Fatal("offline trial missing")
	}
	// Inject outbox insertion failure: records, output and checkpoint must roll back together.
	exec(`CREATE FUNCTION co_o3_fail_plan() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''intentional O3 outbox fault''; END'`)
	exec(`CREATE TRIGGER co_o3_plan_fault BEFORE INSERT ON co_dispatch_plans FOR EACH ROW EXECUTE FUNCTION co_o3_fail_plan()`)
	input := map[string]any{}
	must(json.Unmarshal([]byte(c.Item.InputValues), &input))
	source := &Source{Mode: "offline", Definition: c.Manifest.CollectorDefinition, Fixed: documents[c.StageID]}
	r := collector.ExecuteDurable(ctx, c.Manifest.CollectorDefinition, input, "offline", source, collector.DurableOptions{Commit: func(ctx context.Context, rows []collector.Candidate, cp collector.Checkpoint) error {
		return CommitBatch(ctx, c, rows, cp)
	}})
	if r.Error == nil || r.Error.Code != "BATCH_COMMIT_FAILED" {
		t.Fatal("outbox fault not surfaced", r.Error)
	}
	if count("SELECT COUNT(*) FROM co_run_outputs WHERE run_id=?", offlineID) != 0 || count("SELECT COUNT(*) FROM co_dispatch_plans WHERE run_id=?", offlineID) != 0 || count("SELECT COUNT(*) FROM co_work_items WHERE run_id=? AND checkpoint<>'{}'", offlineID) != 0 {
		t.Fatal("outbox fault escaped transaction")
	}
	exec("DROP TRIGGER co_o3_plan_fault ON co_dispatch_plans")
	exec("DROP FUNCTION co_o3_fail_plan()")
	execute(c, false)
	_, e = ControlRun(ctx, 1, offlineID, "o3-pause-control", "pause")
	must(e)
	must(DispatchRuns(ctx))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", offlineID) != 1 {
		t.Fatal("dispatch during pause")
	}
	_, e = ControlRun(ctx, 1, offlineID, "o3-resume-control", "resume")
	must(e)
	must(DispatchRuns(ctx))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", offlineID) != 2 {
		t.Fatal("resume did not dispatch")
	}
	_, e = ControlRun(ctx, 1, offlineID, "o3-offline-cancel", "cancel")
	must(e)
	// Freeze an explicit URL array plan, then dispatch one element per capacity.
	o, e = StartTask(ctx, 1, taskID, "o3-expand", versionID, "trial")
	must(e)
	expandID := result(o)["id"].(string)
	exec(`UPDATE co_runs SET manifest=jsonb_set(manifest,'{definition,source,inputs}',CAST(? AS jsonb)),input_count=2 WHERE id=?`, collector.JSON([]map[string]any{{"url": "https://fixture.example.test/top250"}, {"url": "https://fixture.example.test/top250"}}), expandID)
	// The extra frozen root is kept for a queue-priority check below.
	extraRoot := uuid.NewString()
	exec(`INSERT INTO co_run_inputs(id,run_id,ordinal,source_key,values_json,source) VALUES(?,?,1,'root:extra',CAST(? AS jsonb),'{}')`, extraRoot, expandID, collector.JSON(map[string]any{"url": "https://fixture.example.test/top250"}))
	must(PrepareRuns(ctx))
	c, e = ClaimWork(ctx, false)
	must(e)
	if c == nil {
		t.Fatal("expansion root missing")
	}
	var expandedDef collector.Definition
	c.Manifest.Stages["ranking"] = FrozenStage{Collector: c.Manifest.Collector, Definition: c.Manifest.CollectorDefinition, Schema: c.Manifest.Schema}
	expandedDef = c.Manifest.CollectorDefinition
	expandedDef.Output.Fields = append(expandedDef.Output.Fields, collector.Field{Key: "urls", Type: "url", Required: true, Multiple: true, EmptyAllowed: true})
	frozen := c.Manifest.Stages["ranking"]
	frozen.Definition = expandedDef
	c.Manifest.Stages["ranking"] = frozen
	c.Manifest.CollectorDefinition = expandedDef
	expandedEdge := c.Manifest.Definition.Edges[0]
	expandedEdge.Expand = &EdgeExpand{Path: "output.urls"}
	expandedEdge.Mapping["url"] = collector.Binding{Kind: "field", Path: "element"}
	c.Manifest.Definition.Edges = []TaskEdge{expandedEdge}
	// SaveBatch works from the frozen claim; update the durable graph to the same edge.
	exec("DELETE FROM co_stage_runs WHERE run_id=? AND stage_id='movie_branch'", expandID)
	c.Manifest.Definition.Stages = c.Manifest.Definition.Stages[:3]
	c.Manifest.Definition.Edges = append(c.Manifest.Definition.Edges, d.Edges[1])
	exec("UPDATE co_runs SET manifest=CAST(? AS jsonb) WHERE id=?", collector.JSON(c.Manifest), expandID)
	values := map[string]any{"movie_id": "expand", "title": "array fixture", "rank": float64(3), "detail_url": "https://fixture.example.test/subject/m001", "urls": []any{"https://fixture.example.test/subject/m001", "https://fixture.example.test/subject/m002"}}
	cp := collector.Checkpoint{Pages: 1, Records: 1, Resume: "url_or_cursor", NextURL: "https://fixture.example.test/top250?start=2", InputHash: canonicalHash(c.Item.InputValues), DefinitionHash: collector.Hash(expandedDef)}
	e = CommitBatch(ctx, c, []collector.Candidate{{Identity: "expand", Valid: true, Values: values}}, cp)
	if e == nil || collector.AsError(e).Code != "BACKPRESSURE" {
		t.Fatal("producer did not yield", e)
	}
	must(FinishWork(ctx, c, collector.Result{Status: "failed", Error: collector.AsError(e)}))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=? AND status='backpressured'", expandID) != 1 {
		t.Fatal("backpressure not persisted")
	}
	must(PrepareRuns(ctx))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=?", expandID) != 1 {
		t.Fatal("new root stole downstream capacity")
	}
	must(DispatchRuns(ctx))
	must(DispatchRuns(ctx))
	if count("SELECT COUNT(*) FROM co_work_items WHERE run_id=? AND status='queued'", expandID) != 1 {
		t.Fatal("array dispatch exceeded capacity")
	}
	child, e := ClaimWork(ctx, false)
	must(e)
	if child == nil || child.StageID != "movie" {
		t.Fatal("array child missing")
	}
	must(FinishWork(ctx, child, collector.Result{Status: "failed", Error: &collector.Error{Code: "FIXTURE_END", Message: "bounded check"}}))
	must(DispatchRuns(ctx))
	if count("SELECT COUNT(*) FROM co_dispatches WHERE run_id=? AND status='delivered'", expandID) != 2 {
		t.Fatal("array not chunked / duplicate dispatch")
	}
	_, e = ControlRun(ctx, 1, expandID, "o3-expand-cancel", "cancel")
	must(e)
	// O4 consumes a real immutable upstream output at a new intermediate entry,
	// retaining its mapping and excluding both the original root and other branch.
	s = engine.NewSession()
	historical, e := s.QueryString(`SELECT o.id,o.run_id FROM co_run_outputs o JOIN co_work_items w ON w.id=o.item_id JOIN co_stage_runs st ON st.id=w.stage_run_id JOIN co_runs r ON r.id=o.run_id JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND st.stage_id='movie' AND r.namespace='formal' AND r.status IN ('succeeded','partial') ORDER BY o.created_at LIMIT 1`, taskID)
	s.Close()
	must(e)
	if len(historical) != 1 {
		t.Fatal("missing graph output for O4 replay")
	}
	o, e = ReplayStage(ctx, 1, historical[0]["run_id"], "o4-graph-output-replay", ReplayRequest{StageID: "comment", OutputIDs: []string{historical[0]["id"]}, Confirmed: true})
	must(e)
	replayID := result(o)["id"].(string)
	must(PrepareRuns(ctx))
	if count("SELECT COUNT(*) FROM co_stage_runs WHERE run_id=? AND stage_id='comment'", replayID) != 1 || count("SELECT COUNT(*) FROM co_stage_runs WHERE run_id=?", replayID) != 1 {
		t.Fatal("local replay recreated upstream/branch")
	}
	s = engine.NewSession()
	rootInput, e := s.QueryString("SELECT source,values_json FROM co_run_inputs WHERE run_id=?", replayID)
	s.Close()
	must(e)
	if len(rootInput) != 1 || !strings.Contains(rootInput[0]["source"], historical[0]["id"]) || !strings.Contains(rootInput[0]["values_json"], "comments") {
		t.Fatal("historical mapping not frozen", rootInput)
	}
	_, e = ControlRun(ctx, 1, replayID, "o4-graph-replay-cancel", "cancel")
	must(e)
}
