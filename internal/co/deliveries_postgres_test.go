package co

import (
	"context"
	"encoding/json"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/plugin"
	"strconv"
	"testing"
	"xorm.io/xorm"
)

// Runs only in the bounded, disposable O2 schema; no external service calls.
func checkO5DeliveryPostgres(t *testing.T, ctx context.Context, engine *xorm.Engine, version string) {
	t.Helper()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(q string, args ...any) { t.Helper(); _, e := engine.Exec(append([]any{q}, args...)...); must(e) }
	count := func(q string, args ...any) int {
		t.Helper()
		rows, e := engine.QueryString(append([]any{q}, args...)...)
		must(e)
		n, e := strconv.Atoi(rows[0]["n"])
		must(e)
		return n
	}
	value := func(o *Operation) map[string]any {
		t.Helper()
		var v map[string]any
		must(json.Unmarshal([]byte(o.Result), &v))
		return v
	}
	rows, e := engine.QueryString("SELECT manifest FROM co_task_versions WHERE id=?", version)
	must(e)
	var m TaskManifest
	must(collector.Strict([]byte(rows[0]["manifest"]), &m))
	def := m.Definition
	def.Source.Inputs = def.Source.Inputs[:1]
	def.Stages[0].Deliveries = []DeliveryBinding{{ID: "download", Port: "records", PluginCode: "aria2", Version: plugin.Aria2DeliveryVersion, Emit: "valid", Mapping: map[string]collector.Binding{"url": {Kind: "field", Path: "output.comments_url"}}}}
	o, e := CreateTask(ctx, 1, "delivery", "delivery-task", def)
	must(e)
	task := value(o)["id"].(string)
	o, e = PublishTask(ctx, 1, task, "delivery-publish", 1)
	must(e)
	vid := value(o)["id"].(string)
	f := &receiptFixture{outcome: "accepted"}
	registry := plugin.NewRegistry()
	must(registry.Register(f))
	deliveryConfig.RLock()
	previous := deliveryConfig.registry
	deliveryConfig.RUnlock()
	ConfigureDeliveries(registry)
	defer ConfigureDeliveries(previous)
	start := func(key, namespace string) *WorkClaim {
		t.Helper()
		o, e := StartTask(ctx, 1, task, key, vid, namespace)
		must(e)
		id := value(o)["id"].(string)
		must(PrepareRuns(ctx))
		c, e := ClaimWork(ctx, false)
		must(e)
		if c == nil || c.Run.ID != id {
			t.Fatal("delivery work missing", id, c)
		}
		return c
	}
	cpFor := func(c *WorkClaim) collector.Checkpoint {
		return collector.Checkpoint{Pages: 1, Records: 1, Resume: "url_or_cursor", InputHash: canonicalHash(c.Item.InputValues), DefinitionHash: collector.Hash(c.Manifest.CollectorDefinition), NextURL: "https://fixture.example.test/page2"}
	}
	candidate := collector.Candidate{Identity: "delivery-1", Valid: true, Page: 1, Values: map[string]any{"movie_id": "delivery-1", "title": "delivery", "rating": 9.2, "comments_url": "https://fixture.example.test/file"}}
	c := start("delivery-formal", "formal")
	cp := cpFor(c)
	exec(`CREATE FUNCTION reject_delivery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$`)
	exec(`CREATE TRIGGER reject_delivery BEFORE INSERT ON co_deliveries FOR EACH ROW EXECUTE FUNCTION reject_delivery()`)
	if e = CommitBatch(ctx, c, []collector.Candidate{candidate}, cp); e == nil {
		t.Fatal("injected outbox failure accepted")
	}
	if count("SELECT COUNT(*) AS n FROM co_run_outputs WHERE run_id=?", c.Run.ID) != 0 || count("SELECT COUNT(*) AS n FROM co_records WHERE values_json->>'movie_id'='delivery-1'") != 0 || count("SELECT COUNT(*) AS n FROM co_work_items WHERE id=? AND checkpoint='{}'", c.Item.ID) != 1 {
		t.Fatal("output, record or checkpoint escaped rollback")
	}
	exec("DROP TRIGGER reject_delivery ON co_deliveries")
	must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
	must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
	if count("SELECT COUNT(*) AS n FROM co_deliveries WHERE run_id=?", c.Run.ID) != 1 || f.submits != 0 {
		t.Fatal("non-atomic or repeated delivery")
	}
	// Completed collection does not prevent a separate delivery worker from claiming.
	must(FinishWork(ctx, c, collector.Result{Status: "succeeded"}))
	first, e := ClaimDelivery(ctx)
	must(e)
	if first == nil || first.Recovery || first.ExternalID != "fixture-gid" || first.DestinationHash != "fixture-service" {
		t.Fatal("send not durably journaled", first)
	}
	exec("UPDATE co_deliveries SET lease_until=NOW()-INTERVAL '1 second' WHERE id=?", first.ID)
	recovered, e := ClaimDelivery(ctx)
	must(e)
	if recovered == nil || !recovered.Recovery || recovered.LeaseToken == first.LeaseToken {
		t.Fatalf("original receipt lease not recovered: first=%+v recovered=%+v", first, recovered)
	}
	if e = finishDelivery(ctx, first, "succeeded", nil, nil); collector.AsError(e).Code != "LEASE_LOST" {
		t.Fatal("old worker overwrote receipt", e)
	}
	status, receipt, problem := runDelivery(ctx, recovered)
	must(finishDelivery(ctx, recovered, status, receipt, problem))
	if status != "succeeded" || f.submits != 0 || f.queries != 1 {
		t.Fatal("recovery resubmitted", status, f)
	}
	page, e := RunPage(ctx, 1, c.Run.ID, "deliveries", "", 20)
	must(e)
	if len(page.(map[string]any)["items"].([]json.RawMessage)) != 1 {
		t.Fatal("receipt page missing")
	}
	if _, e = RunPage(ctx, 2, c.Run.ID, "deliveries", "", 20); e != ErrNotFound {
		t.Fatal("owner leak", e)
	}
	// Pause prevents queued sends; cancellation does not erase sent receipts.
	paused := start("delivery-pause", "formal")
	must(CommitBatch(ctx, paused, []collector.Candidate{candidate}, cpFor(paused)))
	_, e = ControlRun(ctx, 1, paused.Run.ID, "delivery-pause-control", "pause")
	must(e)
	if claim, e := ClaimDelivery(ctx); e != nil || claim != nil {
		t.Fatal("paused run sent", claim, e)
	}
	_, e = ControlRun(ctx, 1, paused.Run.ID, "delivery-cancel-control", "cancel")
	must(e)
	if count("SELECT COUNT(*) AS n FROM co_deliveries WHERE run_id=? AND status='cancelled'", paused.Run.ID) != 1 {
		t.Fatal("queued cancellation missing")
	}
	// Trial enqueues preview evidence only, even with the plugin configured.
	trial := start("delivery-trial", "trial")
	must(CommitBatch(ctx, trial, []collector.Candidate{candidate}, cpFor(trial)))
	must(FinishWork(ctx, trial, collector.Result{Status: "succeeded"}))
	if count("SELECT COUNT(*) AS n FROM co_deliveries WHERE run_id=? AND status='preview'", trial.Run.ID) != 1 {
		t.Fatal("trial not preview")
	}
	if claim, e := ClaimDelivery(ctx); e != nil || claim != nil || f.submits != 0 {
		t.Fatal("trial performed external action", claim, e)
	}
	// Missing configuration is known unsubmitted, allowing only an explicit retry.
	next := start("delivery-unavailable", "formal")
	must(CommitBatch(ctx, next, []collector.Candidate{candidate}, cpFor(next)))
	must(FinishWork(ctx, next, collector.Result{Status: "succeeded"}))
	ConfigureDeliveries(nil)
	if claim, e := ClaimDelivery(ctx); e != nil || claim != nil {
		t.Fatal("unavailable plugin claimed", claim, e)
	}
	rows, e = engine.QueryString("SELECT id,revision FROM co_deliveries WHERE run_id=? AND status='failed' AND external_id=''", next.Run.ID)
	must(e)
	if len(rows) != 1 {
		t.Fatal("unsubmitted failure missing")
	}
	revision, _ := strconv.Atoi(rows[0]["revision"])
	p := DeliveryControlRequest{DeliveryID: rows[0]["id"], Action: "retry", ExpectedRevision: revision, Confirmed: true}
	if _, e = ControlDelivery(ctx, 2, next.Run.ID, "delivery-other-owner", p); e != ErrNotFound {
		t.Fatal("control owner leak", e)
	}
	stale := p
	stale.ExpectedRevision--
	if _, e = ControlDelivery(ctx, 1, next.Run.ID, "delivery-stale", stale); e == nil {
		t.Fatal("stale control accepted")
	}
	ConfigureDeliveries(registry)
	o, e = ControlDelivery(ctx, 1, next.Run.ID, "delivery-retry", p)
	must(e)
	same, e := ControlDelivery(ctx, 1, next.Run.ID, "delivery-retry", p)
	must(e)
	if same.ID != o.ID {
		t.Fatal("duplicate retry operation")
	}
	f.lost = true
	f.outcome = "unknown"
	sending, e := ClaimDelivery(ctx)
	must(e)
	if sending == nil {
		t.Fatal("retry not claimed")
	}
	status, receipt, problem = runDelivery(ctx, sending)
	must(finishDelivery(ctx, sending, status, receipt, problem))
	if status != "awaiting_confirmation" || f.submits != 1 {
		t.Fatal("lost receipt not unknown", status, f)
	}
	p.ExpectedRevision = sending.Revision + 1
	if _, e = ControlDelivery(ctx, 1, next.Run.ID, "delivery-unsafe-retry", p); e == nil {
		t.Fatal("unknown request resent")
	}
	p.Action = "query"
	_, e = ControlDelivery(ctx, 1, next.Run.ID, "delivery-query", p)
	must(e)
	f.outcome = "accepted"
	query, e := ClaimDelivery(ctx)
	must(e)
	if query == nil || !query.Recovery {
		t.Fatal("query not recovery")
	}
	status, receipt, problem = runDelivery(ctx, query)
	must(finishDelivery(ctx, query, status, receipt, problem))
	if f.submits != 1 || status != "succeeded" {
		t.Fatal("query resubmitted", f, status)
	}
}
