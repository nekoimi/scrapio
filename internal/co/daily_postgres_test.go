package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"testing"
	"xorm.io/xorm"
)

func checkO4Postgres(t *testing.T, ctx context.Context, engine *xorm.Engine, taskID, versionID string) {
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
	read := session(ctx)
	v := new(TaskVersion)
	_, e := read.ID(versionID).Get(v)
	must(e)
	read.Close()
	var manifest TaskManifest
	must(collector.Strict([]byte(v.Manifest), &manifest))
	stage := manifest.Stage(manifest.Definition.EntryStage)
	request := SourceRequest{Kind: "table", CollectorVersionID: stage.CollectorVersionID, TableID: stage.Save.TableID, SchemaVersion: stage.Save.SchemaVersion, Limit: 100, Mapping: map[string]string{"url": "detail_url", "movie_id": "movie_id"}}
	// Existing O2 output fixture contains the movie detail input fields.
	for _, field := range manifest.CollectorDefinition.InputFields {
		if field.Key == "url" {
			request.Mapping[field.Key] = "url"
		} else {
			request.Mapping[field.Key] = field.Key
		}
	}
	// Create a source record independently of task save/output fields.
	original := manifest.Definition.Source.Inputs[0]
	exec(`INSERT INTO co_records(id,table_id,canonical_key,values_json,revision,schema_version) VALUES('o4-source',?,'o4-source',CAST(? AS jsonb),1,1)`, stage.Save.TableID, collector.JSON(original))
	request.RecordIDs = []string{"o4-source"}
	o, e := SourcePreview(ctx, 1, "o4-table-preview", request)
	must(e)
	snap := result(o)
	if snap["valid_count"].(float64) != 1 {
		t.Fatal("source mapping invalid", o.Result)
	}
	again, e := SourcePreview(ctx, 1, "o4-table-preview", request)
	must(e)
	if again.ID != o.ID {
		t.Fatal("source retry duplicated")
	}
	if _, e = SourceDetail(ctx, 2, snap["snapshot_id"].(string), 0, 20); e != ErrNotFound {
		t.Fatal("source owner leak", e)
	}
	src := ManualSource{Kind: "table", SnapshotID: snap["snapshot_id"].(string), SnapshotHash: snap["snapshot_hash"].(string), Count: 1}
	exec("UPDATE co_records SET values_json=CAST(? AS jsonb),revision=2 WHERE id='o4-source'", collector.JSON(map[string]any{"url": "https://fixture.example.test/changed"}))
	s := session(ctx)
	must(s.Begin())
	inputs, e := frozenSourceInputs(s, 1, src, manifest.CollectorDefinition)
	must(e)
	if canonicalHash(inputs[0].Values) != collector.Hash(original) {
		t.Fatal("source changed with current table")
	}
	must(s.Commit())
	s.Close()
	content := "[" + collector.JSON(original) + "," + collector.JSON(original) + ",{}]"
	file := SourceRequest{Kind: "file", Format: "json", Content: content, CollectorVersionID: stage.CollectorVersionID, Mapping: request.Mapping}
	o, e = SourcePreview(ctx, 1, "o4-file-preview", file)
	must(e)
	fs := result(o)
	if fs["row_count"].(float64) != 3 || fs["valid_count"].(float64) != 1 || fs["partial"] != true {
		t.Fatal("file duplicates/invalid", o.Result)
	}
	fileSource := ManualSource{Kind: "file", SnapshotID: fs["snapshot_id"].(string), SnapshotHash: fs["snapshot_hash"].(string), Count: 1}
	s = session(ctx)
	must(s.Begin())
	if _, e = frozenSourceInputs(s, 1, fileSource, manifest.CollectorDefinition); e == nil {
		t.Fatal("partial file unconfirmed")
	}
	fileSource.AcceptPartial = true
	_, e = frozenSourceInputs(s, 1, fileSource, manifest.CollectorDefinition)
	must(e)
	must(s.Commit())
	s.Close()
	o, e = SaveSchedule(ctx, 1, taskID, "o4-schedule", ScheduleRequest{TaskVersionID: versionID, IntervalSeconds: 60, Enabled: true})
	must(e)
	exec("UPDATE co_schedules SET next_run_at=NOW()-INTERVAL '1 minute' WHERE task_id=?", taskID)
	must(TickSchedules(ctx))
	must(TickSchedules(ctx))
	s = session(ctx)
	triggers, e := s.QueryString("SELECT status,run_id FROM co_triggers WHERE task_id=? AND kind='schedule'", taskID)
	must(e)
	s.Close()
	if len(triggers) != 1 || triggers[0]["status"] != "started" {
		t.Fatal("schedule duplicate or missing", triggers)
	}
	scheduledRun := triggers[0]["run_id"]
	exec("UPDATE co_schedules SET next_run_at=NOW()-INTERVAL '1 minute' WHERE task_id=?", taskID)
	must(TickSchedules(ctx))
	s = session(ctx)
	triggers, e = s.QueryString("SELECT status FROM co_triggers WHERE task_id=? AND kind='schedule' AND status='skipped_overlap'", taskID)
	must(e)
	s.Close()
	if len(triggers) != 1 {
		t.Fatal("overlap not visible")
	}
	_, e = ControlRun(ctx, 1, scheduledRun, "o4-cancel-schedule", "cancel")
	must(e)
	// A local replay preserves historical input and avoids revisiting upstream.
	s = session(ctx)
	history, e := s.QueryString("SELECT w.id,w.run_id FROM co_work_items w JOIN co_runs r ON r.id=w.run_id JOIN co_task_versions v ON v.id=r.task_version_id WHERE v.task_id=? AND w.status='succeeded' AND r.status IN ('succeeded','partial','failed','limited','cancelled') ORDER BY w.created_at LIMIT 1", taskID)
	must(e)
	s.Close()
	if len(history) == 0 {
		t.Fatal("missing historical fixture")
	}
	o, e = ReplayStage(ctx, 1, history[0]["run_id"], "o4-local-replay", ReplayRequest{StageID: stage.ID, ItemIDs: []string{history[0]["id"]}, Confirmed: true})
	must(e)
	local := result(o)["id"].(string)
	detail, e := RunDetail(ctx, 1, local)
	must(e)
	if detail == nil {
		t.Fatal("missing scoped run")
	}
	_, e = ControlRun(ctx, 1, local, "o4-local-cancel", "cancel")
	must(e)
	// TTL proof is successful collector execution history, not record updated_at.
	manifest.Definition.Stages[0].RefreshSeconds = 60
	o, e = StartTask(ctx, 1, taskID, "o4-refresh-start", versionID, "formal")
	must(e)
	fresh := result(o)["id"].(string)
	exec("UPDATE co_runs SET manifest=CAST(? AS jsonb) WHERE id=?", collector.JSON(manifest), fresh)
	s = session(ctx)
	oldItem := new(WorkItem)
	_, e = s.ID(history[0]["id"]).Get(oldItem)
	must(e)
	r := new(Run)
	_, e = s.ID(fresh).Get(r)
	must(e)
	must(recordSuccessfulInput(s, r, manifest, stage.ID, oldItem))
	s.Close()
	must(PrepareRuns(ctx))
	c, e := ClaimWork(ctx, false)
	must(e)
	if c != nil {
		t.Fatal("fresh input unexpectedly fetched")
	}
	s = session(ctx)
	skipped, e := s.QueryString("SELECT status,error FROM co_work_items WHERE run_id=?", fresh)
	must(e)
	s.Close()
	if len(skipped) != 1 || skipped[0]["status"] != "skipped" {
		t.Fatal("refresh reason missing", skipped)
	}
	_, e = ControlRun(ctx, 1, fresh, "o4-refresh-cancel", "cancel")
	must(e)
	_, e = UpgradePreview(ctx, 1, taskID, UpgradeRequest{StageID: stage.ID, CollectorVersionID: stage.CollectorVersionID})
	must(e)
	_, e = Archive(ctx, 1, manifest.Collector.CollectorID, "o4-reference-archive", 1)
	if e == nil || collector.AsError(e).Code != "CONFLICT" {
		t.Fatal("published collector reference ignored", e)
	}
	must(PrepareRuns(ctx))
	// Source assets not referenced by published tasks can expire; referenced assets cannot.
	exec("UPDATE co_source_snapshots SET created_at=NOW()-INTERVAL '100 days'")
	exec("UPDATE co_tasks SET definition=jsonb_set(definition,'{source}',CAST(? AS jsonb)) WHERE id=?", collector.JSON(src), taskID)
	must(CleanupSources(ctx))
	s = session(ctx)
	protected := new(SourceSnapshot)
	_, e = s.ID(src.SnapshotID).Get(protected)
	must(e)
	expired := new(SourceSnapshot)
	_, e = s.ID(fileSource.SnapshotID).Get(expired)
	must(e)
	s.Close()
	if protected.Expired || !expired.Expired {
		t.Fatal("unsafe retention")
	}
	export, e := ExportTable(ctx, 1, stage.Save.TableID, ExportRequest{RecordIDs: []string{"o4-source"}, Limit: 1})
	must(e)
	if export.(map[string]any)["count"].(int) != 1 {
		t.Fatal("bounded export")
	}
	_, e = Governance(ctx, 1)
	must(e)
	// Capacity rejection cannot partially consume the storage ledger.
	t.Setenv("SCRAPIO_CO_OUTPUT_STORAGE_MIB", "4")
	s = session(ctx)
	must(s.Begin())
	if e = reserveStorage(s, 1, "output_bytes", 4*1024*1024+1); e == nil || collector.AsError(e).Code != "OUTPUT_STORAGE_LIMIT" {
		t.Fatal("output quota not enforced", e)
	}
	must(s.Rollback())
	s.Close()
	// Additive schema upgrades keep record revisions; old bindings explicitly block.
	nextSchema := manifest.Schema
	nextSchema.Fields = append(append([]collector.Field{}, nextSchema.Fields...), collector.Field{Key: "o4_optional", Type: "string", Nullable: true})
	o, e = SchemaUpgrade(ctx, 1, stage.Save.TableID, "o4-schema-add", SchemaUpgradeRequest{ExpectedVersion: 1, Schema: nextSchema, Confirmed: true})
	must(e)
	if result(o)["schema_version"].(float64) != 2 {
		t.Fatal("schema version missing")
	}
	if _, e = StartTask(ctx, 1, taskID, "o4-incompatible-binding", versionID, "formal"); e == nil || collector.AsError(e).Code != "SCHEMA_CONFLICT" {
		t.Fatal("old binding silently upgraded", e)
	}
	_, e = ArchiveTask(ctx, 1, taskID, "o4-archive", 1)
	must(e)
	if _, e = StartTask(ctx, 1, taskID, "o4-archived-trigger", versionID, "formal"); e == nil {
		t.Fatal("archived task started")
	}
}
