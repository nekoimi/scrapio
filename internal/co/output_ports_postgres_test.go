package co

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"xorm.io/xorm"
)

// Runs inside the existing disposable-schema fixture, with its 12s deadline.
func checkO5OutputPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, versionID string) {
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
		var r map[string]any
		must(json.Unmarshal([]byte(o.Result), &r))
		return r
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
	cd := old.CollectorDefinition
	for _, port := range []string{"links", "archive"} {
		p := collector.OutputPort{Records: cd.Records, Output: cd.Output}
		p.Output.Port = port
		cd.AdditionalOutputs = append(cd.AdditionalOutputs, p)
	}
	must(cd.Validate())
	cid, vid, proof := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO co_collectors(id,owner_id,name,revision,definition) VALUES(?,1,'ports',1,CAST(? AS jsonb))`, cid, collector.JSON(cd))
	exec(`INSERT INTO co_operations(id,owner_id,resource_id,operation,key,fingerprint,status) VALUES(?,1,?,'proof','proof','proof','succeeded')`, proof, cid)
	exec(`INSERT INTO co_collector_versions(id,owner_id,collector_id,number,revision,definition,definition_hash,input_schema_hash,output_schema_hash,interpreter_version,publish_version,requirements,evidence_id,check_id) VALUES(?,1,?,1,1,CAST(? AS jsonb),?,?,?,?,'co.publish.v1','{}',?,?)`, vid, cid, collector.JSON(cd), collector.Hash(cd), collector.Hash(cd.InputFields), collector.OutputSchemaHash(cd), collector.Interpreter, proof, proof)
	bind := func(name string) SaveBinding {
		o, e := CreateDataTable(ctx, 1, name, name, old.Schema)
		must(e)
		r := result(o)
		b := old.Definition.Stages[0].Save
		b.TableID = r["id"].(string)
		b.SchemaHash = r["schema_hash"].(string)
		return b
	}
	main, archive := bind("ports-main"), bind("ports-archive")
	d := old.Definition
	d.Source.Inputs = d.Source.Inputs[:1]
	d.Budget.MaxPending = 10
	st := d.Stages[0]
	st.CollectorVersionID = vid
	st.Save = main
	st.AdditionalSaves = []PortSave{{Port: "links", Save: SaveBinding{Mode: "pass_through"}}, {Port: "archive", Save: archive}}
	child := TaskStage{ID: "child", Name: "child", CollectorVersionID: old.Collector.ID, Save: SaveBinding{Mode: "pass_through"}, OnError: "continue"}
	d.Stages = []TaskStage{st, child}
	d.Edges = []TaskEdge{{ID: "links_only", From: st.ID, To: "child", Port: "links", Emit: "valid", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.comments_url"}, "movie_id": {Kind: "field", Path: "output.movie_id"}}}}
	missing := d
	missing.Stages = append([]TaskStage{}, d.Stages...)
	missing.Stages[0].AdditionalSaves = nil
	if _, e := CreateTask(ctx, 1, "missing-port", "missing-port", missing); e == nil {
		t.Fatal("missing explicit port binding accepted")
	}
	o, e := CreateTask(ctx, 1, "ports", "ports-task", d)
	must(e)
	taskID := result(o)["id"].(string)
	o, e = PublishTask(ctx, 1, taskID, "ports-publish", 1)
	must(e)
	published := result(o)["id"].(string)
	o, e = StartTask(ctx, 1, taskID, "ports-run", published, "formal")
	must(e)
	runID := result(o)["id"].(string)
	must(prepareRun(ctx, runID))
	claim, e := ClaimWork(ctx, false)
	must(e)
	if claim == nil || claim.Run.ID != runID {
		t.Fatal("ports claim missing")
	}
	values := map[string]any{"movie_id": "port-movie", "title": "fixture", "rating": 9.2, "comments_url": "https://fixture.example.test/comments"}
	rows := []collector.Candidate{}
	for _, p := range []string{"records", "links", "archive"} {
		rows = append(rows, collector.Candidate{Port: p, Identity: "same", Valid: true, Values: values, Page: 1})
	}
	cp := collector.Checkpoint{Pages: 1, Records: 3, Resume: "url_or_cursor", InputHash: canonicalHash(claim.Item.InputValues), DefinitionHash: collector.Hash(cd), Done: true, FinalStatus: "succeeded"}
	// Fail after the primary business write, before any page can become durable.
	exec(`CREATE FUNCTION fail_port() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.port='links' THEN RAISE EXCEPTION 'injected port failure'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER fail_port BEFORE INSERT ON co_run_outputs FOR EACH ROW EXECUTE FUNCTION fail_port()`)
	if e := CommitBatch(ctx, claim, rows, cp); e == nil {
		t.Fatal("fault did not roll back page")
	}
	if count("SELECT COUNT(*) AS n FROM co_records WHERE table_id IN (?,?)", main.TableID, archive.TableID) != 0 || count("SELECT COUNT(*) AS n FROM co_run_outputs WHERE run_id=?", runID) != 0 || count("SELECT COUNT(*) AS n FROM co_observations WHERE run_id=?", runID) != 0 || count("SELECT COUNT(*) AS n FROM co_work_items WHERE id=? AND checkpoint='{}'", claim.Item.ID) != 1 {
		t.Fatal("partial page persisted")
	}
	exec("DROP TRIGGER fail_port ON co_run_outputs")
	exec("DROP FUNCTION fail_port()")
	must(CommitBatch(ctx, claim, rows, cp))
	must(CommitBatch(ctx, claim, rows, cp))
	if count("SELECT COUNT(*) AS n FROM co_run_outputs WHERE run_id=?", runID) != 3 || count("SELECT COUNT(*) AS n FROM co_records WHERE table_id IN (?,?)", main.TableID, archive.TableID) != 2 || count("SELECT COUNT(*) AS n FROM co_observations WHERE run_id=? AND port='links' AND decision='passed' AND record_id IS NULL", runID) != 1 || count("SELECT COUNT(*) AS n FROM co_dispatch_plans WHERE run_id=?", runID) != 1 {
		t.Fatal("port identity, pass-through or routing broken")
	}
	preview, e := PreviewSave(ctx, 1, vid, SaveBinding{Mode: "pass_through"}, rows[1:2], "links")
	must(e)
	if collector.JSON(preview) == "" {
		t.Fatal("missing pass preview")
	}
	next := old.Schema
	next.Fields = append(append([]collector.Field{}, next.Fields...), collector.Field{Key: "note", Type: "string", Nullable: true})
	if _, e := SchemaUpgrade(ctx, 1, archive.TableID, "ports-schema-active", SchemaUpgradeRequest{ExpectedVersion: 1, Schema: next, Confirmed: true}); e == nil {
		t.Fatal("additional table upgraded during active run")
	}
	must(FinishWork(ctx, claim, collector.Result{Status: "succeeded"}))
	must(dispatchRun(ctx, runID))
	if count("SELECT COUNT(*) AS n FROM co_work_items w JOIN co_stage_runs s ON s.id=w.stage_run_id WHERE w.run_id=? AND s.stage_id='child'", runID) != 1 {
		t.Fatal("pass output not delivered")
	}
	_, e = ControlRun(ctx, 1, runID, "ports-cancel", "cancel")
	must(e)
	outputs, e := engine.QueryString("SELECT id FROM co_run_outputs WHERE run_id=? AND port='records'", runID)
	must(e)
	if _, e := ReplayStage(ctx, 1, runID, "ports-wrong-replay", ReplayRequest{StageID: "child", OutputIDs: []string{outputs[0]["id"]}, Confirmed: true}); collector.AsError(e).Code != "OUTPUT_EVIDENCE_MISSING" {
		t.Fatal("wrong port replay accepted", e)
	}
	o, e = StartTask(ctx, 1, taskID, "ports-trial", published, "trial")
	must(e)
	trialID := result(o)["id"].(string)
	must(prepareRun(ctx, trialID))
	claim, e = ClaimWork(ctx, false)
	must(e)
	if claim == nil || claim.Run.ID != trialID {
		t.Fatal("trial claim missing")
	}
	cp.InputHash = canonicalHash(claim.Item.InputValues)
	cp.DefinitionHash = collector.Hash(claim.Manifest.CollectorDefinition)
	must(CommitBatch(ctx, claim, rows, cp))
	if count("SELECT COUNT(*) AS n FROM co_records WHERE table_id IN (?,?)", main.TableID, archive.TableID) != 2 || count("SELECT COUNT(*) AS n FROM co_record_revisions WHERE record_id IN(SELECT id FROM co_records WHERE table_id IN (?,?))", main.TableID, archive.TableID) != 2 || count("SELECT COUNT(*) AS n FROM co_run_outputs WHERE run_id=? AND namespace='trial'", trialID) != 3 {
		t.Fatal("trial wrote business data or lost outputs")
	}
	_, e = ControlRun(ctx, 1, trialID, "ports-cancel-trial", "cancel")
	must(e)
}
