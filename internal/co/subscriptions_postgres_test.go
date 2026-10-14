package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"strconv"
	"testing"
	"xorm.io/xorm"
)

func checkO5SubscriptionPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, versionID string) {
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
		rows, e := engine.QueryString(append([]any{q}, args...)...)
		must(e)
		n, e := strconv.Atoi(rows[0]["n"])
		must(e)
		return n
	}
	read := session(ctx)
	v := new(TaskVersion)
	_, e := read.ID(versionID).Get(v)
	read.Close()
	must(e)
	var m TaskManifest
	must(collector.Strict([]byte(v.Manifest), &m))
	stage := m.Stage(m.Definition.EntryStage)
	table := stage.Save.TableID
	// O2 created/replayed and rolled-back pages must produce exactly one change.
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=?", table) != 1 {
		t.Fatal("change not atomic with save/replay")
	}
	def := m.Definition
	def.Source.Inputs = def.Source.Inputs[:1]
	def.Stages[0].Save = SaveBinding{Mode: "pass_through"}
	o, e := CreateTask(ctx, 1, "subscriber", "subscription-task", def)
	must(e)
	task := result(o)["id"].(string)
	o, e = PublishTask(ctx, 1, task, "subscription-publish", 1)
	must(e)
	version := result(o)["id"].(string)
	p := SubscriptionRequest{TaskVersionID: version, TableID: table, SchemaVersion: stage.Save.SchemaVersion, SchemaHash: stage.Save.SchemaHash, Mapping: map[string]string{"url": "comments_url", "movie_id": "movie_id"}, InitialMode: "backfill", BatchSize: 1, Enabled: true, Confirmed: true}
	o, e = CreateSubscription(ctx, 1, task, "subscription-create", p)
	must(e)
	subID := result(o)["id"].(string)
	original, e := CreateSubscription(ctx, 1, task, "subscription-create", p)
	must(e)
	if o.ID != original.ID {
		t.Fatal("lost response duplicates subscription")
	}
	if _, e = TaskSubscriptions(ctx, 2, task); e != ErrNotFound {
		t.Fatal("subscription owner isolation")
	}
	if _, e = CreateSubscription(ctx, 1, m.Definition.EntryStage, "wrong-task", p); e == nil {
		t.Fatal("invalid task accepted")
	}
	// Inject failure after run creation: the complete run/cursor/batch transaction rolls back.
	exec(`CREATE FUNCTION subscription_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$`)
	exec(`CREATE TRIGGER subscription_fail BEFORE INSERT ON co_subscription_batches FOR EACH ROW EXECUTE FUNCTION subscription_fail()`)
	before := count("SELECT COUNT(*) AS n FROM co_runs")
	if e = tickSubscription(ctx, subID); e == nil {
		t.Fatal("injected batch failure did not fail")
	}
	if count("SELECT COUNT(*) AS n FROM co_runs") != before || count("SELECT COUNT(*) AS n FROM co_subscriptions WHERE id=? AND backfill_cursor=0", subID) != 1 {
		t.Fatal("subscription advanced on rollback")
	}
	exec("DROP TRIGGER subscription_fail ON co_subscription_batches")
	must(tickSubscription(ctx, subID))
	must(tickSubscription(ctx, subID))
	if count("SELECT COUNT(*) AS n FROM co_subscription_batches WHERE subscription_id=?", subID) != 1 {
		t.Fatal("backfill repeated while run active")
	}
	// Completed handoff is not replayed; source lineage is in immutable run inputs.
	if count(`SELECT COUNT(*) AS n FROM co_run_inputs WHERE source->>'subscription_id'=?`, subID) != 1 {
		t.Fatal("backfill source absent")
	}
	exec("UPDATE co_runs SET status='succeeded' WHERE id IN (SELECT run_id FROM co_subscription_batches WHERE subscription_id=?)", subID)
	o, e = ControlSubscription(ctx, 1, task, "subscription-pause", SubscriptionControl{ID: subID, ExpectedRevision: 1, Action: "pause"})
	must(e)
	// Add a formal source revision through the same event helper under its stream lock.
	addChange := func(revision int, values string) {
		t.Helper()
		s := session(ctx)
		defer s.Close()
		must(s.Begin())
		defer s.Rollback()
		_, e := lockTableStream(s, table)
		must(e)
		record := new(BusinessRecord)
		_, e = s.Where("table_id=?", table).Get(record)
		must(e)
		record.Revision = revision
		record.Values = values
		rows, e := s.QueryString("SELECT observation_id FROM co_record_changes WHERE table_id=? ORDER BY sequence LIMIT 1", table)
		must(e)
		must(appendRecordChange(s, &Run{OwnerID: 1}, record, rows[0]["observation_id"], "updated"))
		must(s.Commit())
	}
	addChange(2, `{"movie_id":"m001","comments_url":"https://fixture.example.test/changed"}`)
	must(tickSubscription(ctx, subID))
	if count("SELECT COUNT(*) AS n FROM co_subscription_batches WHERE subscription_id=?", subID) != 1 {
		t.Fatal("paused consumed")
	}
	_, e = ControlSubscription(ctx, 1, task, "stale-resume", SubscriptionControl{ID: subID, ExpectedRevision: 1, Action: "resume"})
	if e == nil {
		t.Fatal("stale revision accepted")
	}
	_, e = ControlSubscription(ctx, 1, task, "subscription-resume", SubscriptionControl{ID: subID, ExpectedRevision: 2, Action: "resume"})
	must(e)
	must(tickSubscription(ctx, subID))
	must(tickSubscription(ctx, subID))
	if count("SELECT COUNT(*) AS n FROM co_subscription_batches WHERE subscription_id=?", subID) != 2 {
		t.Fatal("change not delivered exactly once")
	}
	exec("UPDATE co_runs SET status='succeeded' WHERE id IN (SELECT run_id FROM co_subscription_batches WHERE subscription_id=?)", subID)
	addChange(3, `{"movie_id":"m001","comments_url":"https://outside.example.test/blocked"}`)
	must(tickSubscription(ctx, subID))
	if count("SELECT COUNT(*) AS n FROM co_subscriptions WHERE id=? AND cursor=2 AND status='blocked' AND enabled=FALSE", subID) != 1 {
		t.Fatal("invalid source advanced cursor")
	}
	// Blocked/paused subscribers pin unconsumed changes, even past retention.
	exec("UPDATE co_record_changes SET created_at=NOW()-INTERVAL '40 days' WHERE table_id=?", table)
	must(CleanupChanges(ctx))
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=? AND sequence=3", table) != 1 {
		t.Fatal("unconsumed change deleted")
	}
	_, e = ControlSubscription(ctx, 1, task, "subscription-retire", SubscriptionControl{ID: subID, ExpectedRevision: 4, Action: "retire"})
	must(e)
	must(CleanupChanges(ctx))
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=?", table) != 0 {
		t.Fatal("retired cursor still pins evidence")
	}
	// A subscription saving back to its own source is rejected, including future mode.
	p.TaskVersionID = versionID
	p.InitialMode = "future"
	_, e = CreateSubscription(ctx, 1, v.TaskID, "subscription-cycle", p)
	if e == nil || collector.AsError(e).Code != "SUBSCRIPTION_CYCLE" {
		t.Fatal("self loop accepted", e)
	}
	// Re-create with future mode at watermark, no existing record replay.
	p.TaskVersionID = version
	p.Enabled = false
	o, e = CreateSubscription(ctx, 1, task, "future-subscription", p)
	must(e)
	if result(o)["cursor"].(float64) != 3 || result(o)["backfill_count"].(float64) != 0 {
		t.Fatal("future start not explicit watermark")
	}
	// Exercise the real save path: trial and unchanged observations do not emit;
	// formal updates emit effective saved values, once despite page replay.
	exec("UPDATE co_runs SET status='cancelled' WHERE status IN ('preparing','running','pausing','paused','awaiting_confirmation')")
	candidate := collector.Candidate{Identity: "m001", Valid: true, Page: 1, Values: map[string]any{"movie_id": "m001", "title": "fixture", "rating": 9.2, "comments_url": "https://fixture.example.test/subject/m001/comments"}}
	save := func(name, namespace string, candidate collector.Candidate) {
		t.Helper()
		o, e := StartTask(ctx, 1, v.TaskID, name, versionID, namespace)
		must(e)
		id := result(o)["id"].(string)
		must(prepareRun(ctx, id))
		c, e := ClaimWork(ctx, false)
		must(e)
		if c == nil {
			t.Fatal("source save work missing")
		}
		cp := collector.Checkpoint{Pages: 1, Records: 1, Resume: "url_or_cursor", InputHash: canonicalHash(c.Item.InputValues), DefinitionHash: collector.Hash(c.Manifest.CollectorDefinition)}
		must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
		must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
		_, e = ControlRun(ctx, 1, id, name+"-cancel", "cancel")
		must(e)
	}
	save("source-unchanged", "formal", candidate)
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=?", table) != 0 {
		t.Fatal("unchanged row emitted")
	}
	candidate.Values["title"] = "new title"
	save("source-trial", "trial", candidate)
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=?", table) != 0 {
		t.Fatal("trial emitted formal change")
	}
	save("source-updated", "formal", candidate)
	if count("SELECT COUNT(*) AS n FROM co_record_changes WHERE table_id=? AND kind='updated' AND values_json->>'title'='new title'", table) != 1 {
		t.Fatal("formal update snapshot absent or repeated")
	}
}
