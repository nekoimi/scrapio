package co

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/spf13/viper"
	"xorm.io/xorm"
)

// One short opt-in check, isolated from application tables and real gateways.
func TestCOAIPostgres(t *testing.T) {
	if os.Getenv("SCRAPIO_CO_AI_DB_TEST") != "1" {
		t.Skip("opt-in disposable PostgreSQL schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	v := viper.New()
	v.SetConfigFile(filepath.Join(root, "config/dev.yaml"))
	if e := v.ReadInConfig(); e != nil {
		t.Fatal("development configuration unavailable")
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
		t.Fatal("database driver unavailable")
	}
	defer engine.Close()
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	if e = engine.DB().PingContext(ctx); e != nil {
		t.Fatal("development PostgreSQL unavailable (bounded connection check)")
	}
	schema := fmt.Sprintf("co_ai_test_%d", time.Now().UnixNano())
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := engine.Exec(append([]any{sql}, args...)...); e != nil {
			t.Fatal(e)
		}
	}
	exec("CREATE SCHEMA " + schema)
	defer engine.Exec("DROP SCHEMA " + schema + " CASCADE")
	exec("SET search_path TO " + schema)
	exec("CREATE TABLE admin(id BIGINT PRIMARY KEY)")
	exec("INSERT INTO admin VALUES(1),(2)")
	for _, name := range []string{"migrate_co_o1.go", "migrate_co_o2.go", "migrate_co_o3.go", "migrate_co_o4.go", "migrate_co_o5_ai.go"} {
		tree, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/db/migrate", name), nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		ast.Inspect(tree, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || len(c.Args) == 0 {
				return true
			}
			f, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || f.Sel.Name != "Exec" {
				return true
			}
			lit, ok := c.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			sql, e := strconv.Unquote(lit.Value)
			if e != nil {
				t.Fatal(e)
			}
			exec(sql)
			return false
		})
	}
	old := databaseEngine
	databaseEngine = func() *xorm.Engine { return engine }
	defer func() { databaseEngine = old }()
	d, src, candidate := aiFixture()
	cid, sid := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO co_collectors(id,owner_id,name,revision,definition) VALUES(?,1,'AI fixture',1,CAST(? AS jsonb))`, cid, collector.JSON(d))
	exec(`INSERT INTO co_samples(id,owner_id,collector_id,revision,input,snapshots,expected_outputs) VALUES(?,1,?,1,CAST(? AS jsonb),CAST(? AS jsonb),'[]')`, sid, cid, collector.JSON(src.Input), collector.JSON(src.Documents))
	calls := 0
	unknown := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if unknown {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				_ = conn.Close()
			}
			return
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if strings.Contains(collector.JSON(p), "credential_ref") {
			t.Error("credential reference sent")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": collector.JSON(candidate)}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 30, "completion_tokens": 10}})
	}))
	defer gateway.Close()
	cfg := &config.AIAssistConfig{Enabled: true, BaseURL: gateway.URL, Model: "fixture", MaxRequestsPerDay: 10}
	prepare := func(key string) *AISuggestion {
		t.Helper()
		o, e := PrepareAI(ctx, 1, key, AIPrepareRequest{Kind: "selectors", ResourceID: cid, ExpectedRevision: 1, SampleID: sid}, cfg)
		if e != nil {
			t.Fatal(e)
		}
		var view map[string]any
		_ = json.Unmarshal([]byte(o.Result), &view)
		s := session(ctx)
		defer s.Close()
		r, e := getAI(s, 1, view["id"].(string), false)
		if e != nil {
			t.Fatal(e)
		}
		if view["prompt_hash"] != canonicalHash(r.Prompt) {
			t.Fatal("PostgreSQL JSONB changed preview hash")
		}
		return r
	}
	row := prepare("prepare-one")
	if _, e := AIView(ctx, 2, row.ID); e != ErrNotFound {
		t.Fatal("owner isolation", e)
	}
	request := AIControlRequest{Confirmed: true, EvidenceHash: canonicalHash(row.Prompt)}
	if _, e = CallAI(ctx, 1, row.ID, "call-one", request, cfg, ai.AssistClient{}); e != nil {
		t.Fatal(e)
	}
	if _, e = CallAI(ctx, 1, row.ID, "call-one", request, cfg, ai.AssistClient{}); e != nil || calls != 1 {
		t.Fatal("idempotency", e, calls)
	}
	if _, e = CallAI(ctx, 1, row.ID, "call-new-key", request, cfg, ai.AssistClient{}); e == nil || calls != 1 {
		t.Fatal("repeat call allowed")
	}
	s := session(ctx)
	row, e = getAI(s, 1, row.ID, false)
	s.Close()
	if e != nil {
		t.Fatal(e)
	}
	var result aiResult
	_ = collector.Strict([]byte(row.Result), &result)
	if !result.Validation.Passed {
		t.Fatal(result)
	}
	control := AIControlRequest{Confirmed: true, ExpectedRevision: 1, EvidenceHash: result.Validation.Hash}
	if _, e = AIControl(ctx, 1, row.ID, "early-apply", "apply", control, cfg); e == nil {
		t.Fatal("apply before validation/accept")
	}
	if _, e = AIControl(ctx, 1, row.ID, "validate", "validate", control, cfg); e != nil {
		t.Fatal(e)
	}
	changed := *cfg
	changed.Model = "changed"
	if _, e = AIControl(ctx, 1, row.ID, "changed-model", "accept", control, &changed); e == nil {
		t.Fatal("changed model accepted")
	}
	if _, e = AIControl(ctx, 1, row.ID, "accept", "accept", control, cfg); e != nil {
		t.Fatal(e)
	}
	control.ExpectedRevision = 2
	if _, e = AIControl(ctx, 1, row.ID, "wrong-revision", "apply", control, cfg); e == nil {
		t.Fatal("revision conflict missed")
	}
	control.ExpectedRevision = 1
	if _, e = AIControl(ctx, 1, row.ID, "apply", "apply", control, cfg); e != nil {
		t.Fatal(e)
	}
	if _, e = AIControl(ctx, 1, row.ID, "apply", "apply", control, cfg); e != nil {
		t.Fatal("apply replay", e)
	}
	current, e := Get(ctx, 1, cid)
	if e != nil || current.Revision != 2 {
		t.Fatal("revision or adoption", current, e)
	}
	// Prepare a second call on the now-current draft, then exercise an unknown
	// response. Neither a new key nor replays may submit it again.
	exec("UPDATE co_collectors SET revision=1 WHERE id=?", cid)
	second := prepare("prepare-unknown")
	unknown = true
	request.EvidenceHash = canonicalHash(second.Prompt)
	if _, e = CallAI(ctx, 1, second.ID, "call-unknown", request, cfg, ai.AssistClient{}); e != nil {
		t.Fatal(e)
	}
	if _, e = CallAI(ctx, 1, second.ID, "call-unknown", request, cfg, ai.AssistClient{}); e != nil || calls != 2 {
		t.Fatal("unknown replay", e, calls)
	}
	if _, e = CallAI(ctx, 1, second.ID, "repeat-unknown", request, cfg, ai.AssistClient{}); e == nil || calls != 2 {
		t.Fatal("unknown resubmitted")
	}
	if _, e = AIControl(ctx, 1, second.ID, "reject", "reject", AIControlRequest{Confirmed: true}, cfg); e != nil {
		t.Fatal(e)
	}
	if _, e = DeleteAI(ctx, 1, second.ID, "cleanup", true); e != nil {
		t.Fatal(e)
	}
	limited := *cfg
	limited.MaxRequestsPerDay = 2
	third := prepare("prepare-over-budget")
	request.EvidenceHash = canonicalHash(third.Prompt)
	if _, e = CallAI(ctx, 1, third.ID, "over-budget", request, &limited, ai.AssistClient{}); e == nil || calls != 2 {
		t.Fatal("deletion reset budget", e)
	}
	// Failure explanations remain read-only even if storage was made ready.
	exec("UPDATE co_ai_suggestions SET kind='failure',status='ready' WHERE id=?", third.ID)
	if _, e = AIControl(ctx, 1, third.ID, "failure-apply", "apply", control, cfg); e == nil {
		t.Fatal("failure explanation changed draft")
	}
	// Even a locally valid mapping must roll back if the full task manifest
	// cannot be rebuilt (for example a referenced version is unavailable).
	tid, aid := uuid.NewString(), uuid.NewString()
	up := d
	up.Output.Fields = []collector.Field{{Key: "url", Type: "url", Required: true}}
	edge := TaskEdge{ID: "edge", From: "entry", To: "sink", Port: "records", Emit: "valid", Mapping: map[string]collector.Binding{}}
	taskDefinition := TaskDefinition{DefinitionVersion: "task.v1", EntryStage: "entry", Source: ManualSource{Kind: "manual", Inputs: []map[string]any{src.Input}}, Stages: []TaskStage{{ID: "entry", Name: "entry", CollectorVersionID: uuid.NewString(), Save: SaveBinding{Mode: "pass_through"}, OnError: "continue"}, {ID: "sink", Name: "sink", CollectorVersionID: uuid.NewString(), Save: SaveBinding{Mode: "pass_through"}, OnError: "continue"}}, Edges: []TaskEdge{edge}, Budget: TaskBudget{MaxWorkItems: 10, MaxOutputs: 10, MaxPending: 10, BrowserSlots: 1, MaxDurationMS: 1000}, OverlapPolicy: "skip"}
	mappingSource := aiSource{Definition: up, Down: d, Edge: edge, Output: map[string]any{"url": "https://fixture.example.test/next"}}
	mappingCandidate := AICandidate{Bindings: []AIBindingEdit{{Field: "url", Path: "output.url"}}}
	mappingRow := AISuggestion{ID: aid, Kind: "mapping", Source: collector.JSON(mappingSource)}
	validation := validateAICandidate(ctx, mappingRow, mappingCandidate)
	if !validation.Passed {
		t.Fatal(validation)
	}
	exec(`INSERT INTO co_tasks(id,owner_id,name,revision,definition) VALUES(?,1,'mapping fixture',1,CAST(? AS jsonb))`, tid, collector.JSON(taskDefinition))
	exec(`INSERT INTO co_ai_suggestions(id,owner_id,resource_kind,resource_id,kind,base_revision,base_hash,source,prompt,model,gateway_hash,status,result,validated_hash) VALUES(?,1,'task',?,'mapping',1,?,CAST(? AS jsonb),'{}','fixture',?,'accepted',CAST(? AS jsonb),?)`, aid, tid, collector.Hash(taskDefinition), mappingRow.Source, aiConfigHash(cfg), collector.JSON(aiResult{Candidate: mappingCandidate, Validation: validation}), validation.Hash)
	if _, e = AIControl(ctx, 1, aid, "mapping-rollback", "apply", AIControlRequest{Confirmed: true, ExpectedRevision: 1, EvidenceHash: validation.Hash}, cfg); e == nil {
		t.Fatal("invalid manifest adopted")
	}
	unchanged, e := GetTask(ctx, 1, tid)
	if e != nil || unchanged.Revision != 1 || canonicalHash(unchanged.Definition) != canonicalHash(collector.JSON(taskDefinition)) {
		t.Fatal("failed task adoption did not roll back", e)
	}
}
