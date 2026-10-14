package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/spf13/viper"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"xorm.io/xorm"
)

// Opt-in, short PostgreSQL invariant check. All tables live in a disposable
// schema; application/public tables and real sites are never accessed.
func TestCOO2Postgres(t *testing.T) {
	t.Setenv("SCRAPIO_CO_SITE_INTERVAL_MS", "0")
	if os.Getenv("SCRAPIO_CO_O2_DB_TEST") != "1" {
		t.Skip("opt-in disposable PostgreSQL schema check")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	v := viper.New()
	v.SetConfigFile(filepath.Join(root, "config/dev.yaml"))
	if e := v.ReadInConfig(); e != nil {
		t.Fatal(e)
	}
	dsn := v.GetString("db.dsn")
	if u, e := url.Parse(dsn); e == nil && u.Host != "" {
		q := u.Query()
		q.Set("connect_timeout", "2")
		q.Set("statement_timeout", "3000")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " connect_timeout=2 statement_timeout=3000"
	}
	engine, e := xorm.NewEngine("postgres", dsn)
	if e != nil {
		t.Fatal("cannot initialize development PostgreSQL driver")
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if e = engine.DB().PingContext(ctx); e != nil {
		t.Fatal("development PostgreSQL unavailable (bounded connection check)")
	}
	schema := fmt.Sprintf("co_o2_test_%d", time.Now().UnixNano())
	exec := func(q string, args ...any) {
		t.Helper()
		all := append([]any{q}, args...)
		if _, e := engine.Exec(all...); e != nil {
			t.Fatalf("test SQL failed: %v", e)
		}
	}
	exec("CREATE SCHEMA " + schema)
	defer engine.Exec("DROP SCHEMA " + schema + " CASCADE")
	exec("SET search_path TO " + schema)
	exec("CREATE TABLE admin(id BIGINT PRIMARY KEY)")
	exec("INSERT INTO admin(id) VALUES(1),(2)")
	for _, name := range []string{"migrate_co_o1.go", "migrate_co_o2.go", "migrate_co_o3.go", "migrate_co_o4.go", "migrate_co_o5.go", "migrate_co_o5_outputs.go"} {
		tree, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/db/migrate", name), nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Exec" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			sql, e := strconv.Unquote(literal.Value)
			if e != nil {
				t.Fatal(e)
			}
			exec(sql)
			found = true
			return false
		})
		if !found {
			t.Fatal("migration SQL missing", name)
		}
	}
	previous := databaseEngine
	databaseEngine = func() *xorm.Engine { return engine }
	defer func() { databaseEngine = previous }()
	var fixtures struct {
		Collectors []struct {
			Definition collector.Definition `json:"definition"`
		} `json:"collectors"`
	}
	raw, e := os.ReadFile(filepath.Join(root, "docs/项目文档v2.2/编排O0/fixtures/baseline.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = collector.Strict(raw, &map[string]any{}); e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal(raw, &fixtures)
	d := fixtures.Collectors[1].Definition
	d.Fetch.Kind = "http_html"
	d.Fetch.Method = "GET"
	if e = d.Validate(); e != nil {
		t.Fatal(e)
	}
	cid, vid, proof := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO co_collectors(id,owner_id,name,revision,definition) VALUES(?,1,'test',1,CAST(? AS jsonb))`, cid, collector.JSON(d))
	exec(`INSERT INTO co_operations(id,owner_id,resource_id,operation,key,fingerprint,status) VALUES(?,1,?,'proof','proof','proof','succeeded')`, proof, cid)
	exec(`INSERT INTO co_collector_versions(id,owner_id,collector_id,number,revision,definition,definition_hash,input_schema_hash,output_schema_hash,interpreter_version,publish_version,requirements,evidence_id,check_id) VALUES(?,1,?,1,1,CAST(? AS jsonb),?,?,?,?,'co.publish.v1','{}',?,?)`, vid, cid, collector.JSON(d), collector.Hash(d), collector.Hash(d.InputFields), collector.OutputSchemaHash(d), collector.Interpreter, proof, proof)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	value := func(o *Operation) map[string]any {
		t.Helper()
		var result map[string]any
		must(json.Unmarshal([]byte(o.Result), &result))
		return result
	}
	o, e := CreateDataTable(ctx, 1, "movies", "table", TableSchema{Fields: d.Output.Fields, UniqueKey: []string{"movie_id"}})
	must(e)
	table := value(o)
	b := SaveBinding{TableID: table["id"].(string), SchemaVersion: 1, SchemaHash: table["schema_hash"].(string), UniqueKey: []string{"movie_id"}, UpdatePolicy: "merge_present", NullPolicy: "keep", Mapping: map[string]collector.Binding{}}
	for _, f := range d.Output.Fields {
		b.Mapping[f.Key] = collector.Binding{Kind: "field", Path: "output." + f.Key}
	}
	definition := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "movie", Source: ManualSource{Kind: "manual", Inputs: []map[string]any{{"url": "https://fixture.example.test/subject/m001", "movie_id": "m001"}, {"url": "https://fixture.example.test/subject/m002", "movie_id": "m002"}}}, Stages: []TaskStage{{ID: "movie", Name: "movie", CollectorVersionID: vid, Save: b, OnError: "continue"}}, Edges: []TaskEdge{}, Budget: TaskBudget{MaxWorkItems: 100, MaxOutputs: 100, MaxPending: 1, BrowserSlots: 1, MaxDurationMS: 600000}, OverlapPolicy: "skip"}
	o, e = CreateTask(ctx, 1, "movies", "task", definition)
	must(e)
	taskID := value(o)["id"].(string)
	same, e := CreateTask(ctx, 1, "movies", "task", definition)
	must(e)
	if same.ID != o.ID {
		t.Fatal("duplicate task operation")
	}
	if _, e = CreateTask(ctx, 1, "changed", "task", definition); e == nil {
		t.Fatal("fingerprint not checked")
	}
	if _, e = GetTask(ctx, 2, taskID); e != ErrNotFound {
		t.Fatal("owner leak", e)
	}
	if _, e = SaveTask(ctx, 1, taskID, 0, definition); e == nil {
		t.Fatal("revision conflict missing")
	}
	o, e = PublishTask(ctx, 1, taskID, "publish", 1)
	must(e)
	taskVersion := value(o)["id"].(string)
	o, e = StartTask(ctx, 1, taskID, "run", taskVersion, "formal")
	must(e)
	runID := value(o)["id"].(string)
	same, e = StartTask(ctx, 1, taskID, "run", taskVersion, "formal")
	must(e)
	if same.ID != o.ID {
		t.Fatal("trigger idempotency")
	}
	must(PrepareRuns(ctx))
	c, e := ClaimWork(ctx, false)
	must(e)
	if c == nil || c.Run.ID != runID {
		t.Fatal("claim missing")
	}
	must(Heartbeat(ctx, c))
	must(JournalCommand(ctx, c, "fixture-command", false))
	receiptAttempt, e := OriginalCommand(ctx, 1, runID, c.Item.ID, c.Attempt.ID, "fixture-command")
	must(e)
	if receiptAttempt.ID != c.Attempt.ID {
		t.Fatal("original receipt reference changed")
	}
	must(JournalCommand(ctx, c, "fixture-command", true))
	if _, e = ControlRun(ctx, 1, runID, "stale-control", "cancel", 99); e == nil {
		t.Fatal("stale control epoch accepted")
	}
	candidate := collector.Candidate{Identity: "m001", Valid: true, Page: 1, Values: map[string]any{"movie_id": "m001", "title": "fixture", "rating": 9.2, "comments_url": "https://fixture.example.test/subject/m001/comments"}}
	cp := collector.Checkpoint{Pages: 1, Records: 1, Resume: "url_or_cursor", InputHash: canonicalHash(c.Item.InputValues), DefinitionHash: collector.Hash(d), NextURL: "https://fixture.example.test/page2"}
	exec("UPDATE co_work_items SET lease_token='stolen' WHERE id=?", c.Item.ID)
	if e = CommitBatch(ctx, c, []collector.Candidate{candidate}, cp); collector.AsError(e).Code != "LEASE_LOST" {
		t.Fatal("old lease wrote", e)
	}
	exec("UPDATE co_work_items SET lease_token=? WHERE id=?", c.Item.LeaseToken, c.Item.ID)
	must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
	must(CommitBatch(ctx, c, []collector.Candidate{candidate}, cp))
	count := func(table string) int {
		t.Helper()
		rows, e := engine.QueryString("SELECT COUNT(*) AS n FROM " + table)
		must(e)
		n, _ := strconv.Atoi(rows[0]["n"])
		return n
	}
	if count("co_records") != 1 || count("co_run_outputs") != 1 || count("co_observations") != 1 {
		t.Fatal("duplicate committed output")
	}
	preview, e := PreviewSave(ctx, 1, vid, b, []collector.Candidate{candidate})
	must(e)
	if !strings.Contains(collector.JSON(preview), "unchanged") {
		t.Fatal("unchanged preview missing", preview)
	}
	_, e = TaskList(ctx, 1, "", 20)
	must(e)
	_, e = Tables(ctx, 1, "", 20)
	must(e)
	_, e = TableView(ctx, 1, b.TableID)
	must(e)
	_, e = CollectorVersion(ctx, 1, vid)
	must(e)
	_, e = Runs(ctx, 1, "", 20)
	must(e)
	_, e = TaskVersions(ctx, 1, taskID)
	must(e)
	for _, kind := range []string{"work-items", "outputs", "observations"} {
		_, e = RunPage(ctx, 1, runID, kind, "", 20)
		must(e)
	}
	_, e = ItemDetail(ctx, 1, runID, c.Item.ID)
	must(e)
	_, e = TableRecords(ctx, 1, b.TableID, "", 20)
	must(e)
	records, e := engine.QueryString("SELECT id FROM co_records LIMIT 1")
	must(e)
	_, e = RecordHistory(ctx, 1, records[0]["id"])
	must(e)
	changed := candidate
	changed.Values = map[string]any{"movie_id": "m001", "title": "changed", "rating": 9.2, "comments_url": "https://fixture.example.test/subject/m001/comments"}
	if e = CommitBatch(ctx, c, []collector.Candidate{changed}, cp); collector.AsError(e).Code != "OUTPUT_CHANGED_ON_REPLAY" {
		t.Fatal("immutable output overwritten", e)
	}
	exec(`CREATE FUNCTION reject_batch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='batch_committed' THEN RAISE EXCEPTION 'injected batch failure'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER fail_batch BEFORE INSERT ON co_run_events FOR EACH ROW EXECUTE FUNCTION reject_batch()`)
	next := candidate
	next.Identity = "m003"
	next.Values = map[string]any{"movie_id": "m003", "title": "rollback", "rating": 8.1, "comments_url": "https://fixture.example.test/subject/m003/comments"}
	nextCP := cp
	nextCP.Pages = 2
	if e = CommitBatch(ctx, c, []collector.Candidate{next}, nextCP); e == nil || collector.AsError(e).Code != "BATCH_COMMIT_FAILED" || !collector.AsError(e).Retryable {
		t.Fatal("injected failure not classified as recoverable batch uncertainty", e)
	}
	if count("co_records") != 1 || count("co_observations") != 1 || count("co_run_outputs") != 1 || count("co_record_revisions") != 1 {
		t.Fatal("batch not rolled back")
	}
	rows, e := engine.QueryString("SELECT checkpoint ->> 'pages' AS page FROM co_work_items WHERE id=?", c.Item.ID)
	must(e)
	if rows[0]["page"] != "1" {
		t.Fatal("checkpoint escaped rollback")
	}
	exec("DROP TRIGGER fail_batch ON co_run_events")
	_, e = ControlRun(ctx, 1, runID, "pause", "pause")
	must(e)
	if e = CommitBatch(ctx, c, nil, cp); collector.AsError(e).Code != "CONTROL_PAUSED" {
		t.Fatal("pause did not stop at batch boundary", e)
	}
	detail, e := RunDetail(ctx, 1, runID)
	must(e)
	if collector.JSON(detail) == "" {
		t.Fatal("detail missing")
	}
	_, e = ControlRun(ctx, 1, runID, "resume", "resume")
	must(e)
	c2, e := ClaimWork(ctx, false)
	must(e)
	if c2 == nil || c2.Item.LeaseToken == c.Item.LeaseToken {
		t.Fatal("lease not changed")
	}
	cp.Done = true
	cp.FinalStatus = "succeeded"
	must(CommitBatch(ctx, c2, nil, cp))
	must(FinishWork(ctx, c2, collector.Result{Status: "succeeded"}))
	must(PrepareRuns(ctx))
	c3, e := ClaimWork(ctx, false)
	must(e)
	if c3 == nil || c3.Item.InputValues == c.Item.InputValues {
		t.Fatal("second root input missing")
	}
	must(FinishWork(ctx, c3, collector.Result{Status: "failed", Error: &collector.Error{Code: "INPUT_FIELD_INVALID", Message: "intentional"}}))
	o, e = RetryFailed(ctx, 1, runID, "retry", true)
	must(e)
	retryID := value(o)["id"].(string)
	if retryID == runID || value(o)["input_count"] != float64(1) {
		t.Fatal("not a failed-only new run")
	}
	_, e = ControlRun(ctx, 1, retryID, "cancel", "cancel")
	must(e)
	// Same definition, isolated trial namespace: no business updates/revisions.
	o, e = StartTask(ctx, 1, taskID, "trial", taskVersion, "trial")
	must(e)
	trialID := value(o)["id"].(string)
	must(PrepareRuns(ctx))
	trial, e := ClaimWork(ctx, false)
	must(e)
	if trial == nil || trial.Run.ID != trialID {
		t.Fatal("trial claim")
	}
	trialCP := cp
	trialCP.InputHash = canonicalHash(trial.Item.InputValues)
	must(CommitBatch(ctx, trial, []collector.Candidate{changed}, trialCP))
	if count("co_records") != 1 || count("co_record_revisions") != 1 {
		t.Fatal("trial wrote business data")
	}
	_, e = ControlRun(ctx, 1, trialID, "cancel-trial", "cancel")
	must(e)
	// Schema guard is checked at commit, not just publication.
	o, e = StartTask(ctx, 1, taskID, "schema", taskVersion, "formal")
	must(e)
	schemaRun := value(o)["id"].(string)
	must(PrepareRuns(ctx))
	schemaClaim, e := ClaimWork(ctx, false)
	must(e)
	exec("UPDATE co_data_tables SET current_schema_version=2 WHERE id=?", b.TableID)
	schemaCP := cp
	schemaCP.InputHash = canonicalHash(schemaClaim.Item.InputValues)
	if e = CommitBatch(ctx, schemaClaim, []collector.Candidate{candidate}, schemaCP); collector.AsError(e).Code != "SCHEMA_CONFLICT" {
		t.Fatal("schema not fenced", e)
	}
	exec("UPDATE co_data_tables SET current_schema_version=1 WHERE id=?", b.TableID)
	_, e = ControlRun(ctx, 1, schemaRun, "cancel-schema", "cancel")
	must(e)
	// Browser expiry before a safe checkpoint is never claimed as a fresh action.
	browserManifest := schemaClaim.Manifest
	browserManifest.CollectorDefinition.Fetch.Kind = "browser"
	browserManifest.Stages = nil // Explicit O2 legacy manifest exercises compatibility.
	exec("UPDATE co_runs SET status='running',manifest=CAST(? AS jsonb) WHERE id=?", collector.JSON(browserManifest), schemaRun)
	exec("UPDATE co_work_items SET status='running',lease_token='lost',lease_until=NOW()-INTERVAL '1 second' WHERE id=?", schemaClaim.Item.ID)
	recovery, e := ClaimWork(ctx, true)
	must(e)
	if recovery == nil || !recovery.Recovery {
		t.Fatal("browser interruption not awaiting confirmation")
	}
	still, e := ClaimWork(ctx, true)
	must(e)
	if still != nil {
		t.Fatal("unknown browser action replayed")
	}
	_, e = ControlRun(ctx, 1, schemaRun, "cancel-unknown", "cancel")
	must(e)
	// Interrupted read-only items retain the committed checkpoint and acquire a new token.
	o, e = StartTask(ctx, 1, taskID, "read-recovery", taskVersion, "formal")
	must(e)
	readRun := value(o)["id"].(string)
	must(PrepareRuns(ctx))
	oldClaim, e := ClaimWork(ctx, false)
	must(e)
	if oldClaim == nil || oldClaim.Run.ID != readRun {
		t.Fatal("read recovery claim missing")
	}
	readCP := cp
	readCP.Done = false
	readCP.FinalStatus = ""
	readCP.InputHash = canonicalHash(oldClaim.Item.InputValues)
	must(CommitBatch(ctx, oldClaim, nil, readCP))
	exec("UPDATE co_work_items SET lease_until=NOW()-INTERVAL '1 second' WHERE id=?", oldClaim.Item.ID)
	expired, e := ClaimWork(ctx, false)
	must(e)
	if expired != nil {
		t.Fatal("expired attempt not recovered first")
	}
	newClaim, e := ClaimWork(ctx, false)
	must(e)
	if newClaim == nil || newClaim.Item.LeaseToken == oldClaim.Item.LeaseToken || newClaim.Item.Checkpoint == "{}" {
		t.Fatal("checkpoint/token not recovered")
	}
	if e = Heartbeat(ctx, oldClaim); collector.AsError(e).Code != "LEASE_LOST" {
		t.Fatal("old heartbeat accepted", e)
	}
	_, e = ControlRun(ctx, 1, readRun, "cancel-read", "cancel")
	must(e)
	if _, e = RunEvents(ctx, 2, runID, 0); e != ErrNotFound {
		t.Fatal("event owner isolation", e)
	}
	if os.Getenv("SCRAPIO_CO_O5_OUTPUT_DB_TEST") == "1" {
		checkO5OutputPostgres(t, ctx, engine, taskVersion)
	} else if os.Getenv("SCRAPIO_CO_O5_DB_TEST") == "1" {
		checkO5Postgres(t, ctx, engine, taskID, taskVersion)
	} else if os.Getenv("SCRAPIO_CO_O4_DB_TEST") == "1" {
		checkO4Postgres(t, ctx, engine, taskID, taskVersion)
	} else {
		checkO3Postgres(t, ctx, root, engine)
	}
	must(ctx.Err())
}
