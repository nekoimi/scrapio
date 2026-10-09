package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/migrate"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	pb "github.com/nekoimi/scrapio/internal/drission_rod/grpc"
	"github.com/nekoimi/scrapio/internal/pkg/jwt"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"xorm.io/xorm"
)

// The RPC fixture isolates Go ownership/concurrency/HTTP behavior. Real Python
// protocol and local Chromium coverage lives in scrapio-browser's tests.
type editorRPCFixture struct {
	pb.UnimplementedPageFetchServiceServer
	mu               sync.Mutex
	sessions         map[string]*pb.EditorSessionResponse
	heartbeatStarted chan struct{}
	heartbeatRelease chan struct{}
	closeFails       bool
}

func (f *editorRPCFixture) CreateEditorSession(_ context.Context, r *pb.EditorSessionCreateRequest) (*pb.EditorSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prior := f.sessions[r.SessionId]; prior != nil {
		return prior, nil
	}
	state := &pb.EditorSessionResponse{Success: true, SessionId: r.SessionId, Status: "ready", CurrentUrl: r.Url, PageStateId: r.SessionId + ":1", ExpiresAtUnixMs: time.Now().Add(2 * time.Minute).UnixMilli(), ViewportWidth: r.ViewportWidth, ViewportHeight: r.ViewportHeight}
	f.sessions[r.SessionId] = state
	return state, nil
}
func (f *editorRPCFixture) GetEditorSession(_ context.Context, r *pb.EditorSessionRequest) (*pb.EditorSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state := f.sessions[r.SessionId]; state != nil {
		return state, nil
	}
	return &pb.EditorSessionResponse{ErrorCode: "SESSION_NOT_FOUND", Error: "missing"}, nil
}
func (f *editorRPCFixture) HeartbeatEditorSession(ctx context.Context, r *pb.EditorSessionRequest) (*pb.EditorSessionResponse, error) {
	state, err := f.GetEditorSession(ctx, r)
	if f.heartbeatStarted != nil {
		close(f.heartbeatStarted)
		<-f.heartbeatRelease
	}
	return state, err
}
func (f *editorRPCFixture) CloseEditorSession(_ context.Context, r *pb.EditorSessionRequest) (*pb.EditorSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeFails {
		f.closeFails = false
		return &pb.EditorSessionResponse{ErrorCode: "BROWSER_UNAVAILABLE", Error: "close failed"}, nil
	}
	delete(f.sessions, r.SessionId)
	return &pb.EditorSessionResponse{Success: true, SessionId: r.SessionId, Status: "closed"}, nil
}
func (f *editorRPCFixture) FrameEditorSession(ctx context.Context, r *pb.EditorSessionRequest) (*pb.EditorSessionResponse, error) {
	state, err := f.GetEditorSession(ctx, r)
	if !state.Success {
		return state, err
	}
	if r.PageStateId != state.PageStateId {
		return &pb.EditorSessionResponse{ErrorCode: "STALE_PAGE_STATE", Error: "stale"}, nil
	}
	copy := *state
	copy.Screenshot, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aOc0AAAAASUVORK5CYII=")
	return &copy, nil
}

func TestV22A02A03DevDatabase(t *testing.T) {
	if os.Getenv("SCRAPIO_RUN_DEV_DB_MIGRATION_TEST") != "1" {
		t.Skip("opt in with SCRAPIO_RUN_DEV_DB_MIGRATION_TEST=1")
	}
	oldLogLevel := log.GetLevel()
	log.SetLevel(log.WarnLevel)
	defer log.SetLevel(oldLogLevel)
	_, source, _, _ := runtime.Caller(0)
	v := viper.New()
	v.SetConfigFile(filepath.Join(filepath.Dir(source), "..", "..", "config", "dev.yaml"))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	setup, err := xorm.NewEngine("postgres", v.GetString("db.dsn"))
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	schema := fmt.Sprintf("scrapio_v22_api_test_%d", time.Now().UnixNano())
	if _, err := setup.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := setup.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	// Every pooled connection uses only the temporary schema, never public.
	dsn := v.GetString("db.dsn")
	if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		q := parsed.Query()
		q.Set("search_path", schema)
		parsed.RawQuery = q.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	fixtureDB, err := xorm.NewEngine("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fixtureDB.Close()
	fixtureDB.SetSchema(schema)
	if err := fixtureDB.CreateTables(new(table.Admin), new(table.Migrates)); err != nil {
		t.Fatal(err)
	}
	// Execute the actual A02 -> A03 migrations twice to verify restart safety.
	for pass := 0; pass < 2; pass++ {
		for _, m := range migrate.GetAll() {
			if m.Version() >= 20260929001 {
				if err := m.Exec(fixtureDB); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// Legacy migrations are outside this fixture's scope. Seed test-only ledger
	// entries so the ordinary lifecycle connects to the isolated A02/A03 schema.
	for _, m := range migrate.GetAll() {
		if _, err := fixtureDB.InsertOne(&table.Migrates{Version: m.Version(), Success: true, Message: "isolated test fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	admins := []table.Admin{{Username: "owner-a"}, {Username: "owner-b"}}
	for i := range admins {
		if _, err := fixtureDB.InsertOne(&admins[i]); err != nil {
			t.Fatal(err)
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	service := &editorRPCFixture{sessions: make(map[string]*pb.EditorSessionResponse)}
	grpcServer := grpc.NewServer()
	pb.RegisterPageFetchServiceServer(grpcServer, service)
	go grpcServer.Serve(listener)
	defer grpcServer.Stop()
	cfg := &config.Config{DB: &config.DBConfig{Dsn: dsn}, Crawler: &config.CrawlerConfig{DrissionRodGrpcIp: "127.0.0.1", DrissionRodGrpcPort: listener.Addr().(*net.TCPAddr).Port}}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, cfg)
	lifecycle := db.NewDBLifecycle()
	if err := lifecycle.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer lifecycle.Stop(ctx)
	db.Instance().ShowSQL(false)
	rows, err := db.Instance().QueryString("SELECT current_schema() AS name")
	if err != nil || len(rows) != 1 || rows[0]["name"] != schema {
		t.Fatal("schema isolation failed", err)
	}
	browser := drission_rod.NewDrissionRod()
	if err := browser.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer browser.Stop(ctx)
	bean.MustRegisterPtr(ctx, browser)
	jwt.SetSecret("isolated-v22-integration-test-secret")
	tokens := make([]string, 2)
	for i := range admins {
		token, err := jwt.NewToken(&admins[i])
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = token.Token
	}
	router := newRouter(ctx, cfg)
	static := http.FileServer(http.Dir(filepath.Join(filepath.Dir(source), "..", "..", "web", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			router.ServeHTTP(w, r)
		} else {
			static.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	client := &http.Client{Timeout: 8 * time.Second}
	call := func(owner int, method, path, key string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequest(method, server.URL+"/api/v3"+path, bytes.NewReader(raw))
		if err != nil {
			t.Error(err)
			return 0, nil
		}
		req.Header.Set("Authorization", tokens[owner])
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Error(err)
			return 0, nil
		}
		defer res.Body.Close()
		var result map[string]any
		if res.StatusCode != 204 {
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Error(err)
			}
		}
		return res.StatusCode, result
	}
	input := map[string]any{"entry_url": "https://example.test/list", "entry_type": "web", "name": "fixture"}
	const parallel = 6
	results := make(chan string, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, res := call(0, "POST", "/collectors", "same-create", input)
			if code != 201 {
				t.Errorf("concurrent create: %d %v", code, res)
				return
			}
			results <- res["data"].(map[string]any)["id"].(string)
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	for value := range results {
		if id != "" && id != value {
			t.Fatal("duplicate collector")
		}
		id = value
	}
	if id == "" {
		t.Fatal("collector not created")
	}
	input["name"] = "other"
	if code, _ := call(0, "POST", "/collectors", "same-create", input); code != 409 {
		t.Fatal("create fingerprint not enforced", code)
	}
	if code, _ := call(1, "GET", "/collectors/"+id+"/draft", "", nil); code != 404 {
		t.Fatal("collector owner leak", code)
	}
	definition := map[string]any{"definition_version": 1, "entry_url": input["entry_url"], "steps": []any{}}
	update := map[string]any{"name": "edited", "expected_revision": 1, "definition": definition}
	if code, _ := call(0, "PUT", "/collectors/"+id+"/draft", "", update); code != 200 {
		t.Fatal("update", code)
	}
	if code, res := call(0, "PUT", "/collectors/"+id+"/draft", "", update); code != 409 || res["request_id"] == nil {
		t.Fatal("revision conflict", code, res)
	}
	if code, res := call(0, "POST", "/collectors/"+id+"/validate", "", nil); code != 200 || res["data"].(map[string]any)["validation"].(map[string]any)["valid"] != true {
		t.Fatal("validation", code, res)
	}
	code, copy := call(0, "POST", "/collectors/"+id+"/copies", "copy", nil)
	if code != 201 {
		t.Fatal("copy", code)
	}
	copyID := copy["data"].(map[string]any)["id"].(string)
	if copyID == id || copy["data"].(map[string]any)["revision"] != float64(1) || copy["data"].(map[string]any)["validation_status"] != "not_validated" {
		t.Fatal("copy isolation", copy)
	}
	if code, _ := call(0, "POST", "/collectors/"+copyID+"/copies", "copy", nil); code != 409 {
		t.Fatal("copy fingerprint", code)
	}
	sessionInput := map[string]any{"collector_id": id, "draft_revision": 2, "viewport": map[string]int{"width": 1280, "height": 800}}
	sessionIDs := make(chan string, parallel)
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, res := call(0, "POST", "/browser-sessions", "same-session", sessionInput)
			if code != 201 && code != 200 {
				t.Errorf("session create %d %v", code, res)
				return
			}
			sessionIDs <- res["data"].(map[string]any)["session_id"].(string)
		}()
	}
	wg.Wait()
	close(sessionIDs)
	sessionID := ""
	for value := range sessionIDs {
		if sessionID != "" && sessionID != value {
			t.Fatal("duplicate session")
		}
		sessionID = value
	}
	if sessionID == "" {
		t.Fatal("no session")
	}
	path := "/browser-sessions/" + sessionID
	if code, _ := call(1, "GET", path, "", nil); code != 404 {
		t.Fatal("session owner leak", code)
	}
	sessionInput["viewport"] = map[string]int{"width": 1024, "height": 768}
	if code, _ := call(0, "POST", "/browser-sessions", "same-session", sessionInput); code != 409 {
		t.Fatal("viewport fingerprint", code)
	}
	if code, _ := call(0, "GET", path+"/frame?page_state_id=stale", "", nil); code != 409 {
		t.Fatal("stale frame", code)
	}
	// A real network read must see the first event without waiting for handler
	// completion; this exercises every middleware in the production router.
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/api/v3"+path+"/events", nil)
	req.Header.Set("Authorization", tokens[0])
	stream, err := client.Do(req)
	if err != nil {
		t.Fatal("SSE flush failed", err)
	}
	if stream.StatusCode != 200 {
		body, _ := io.ReadAll(stream.Body)
		t.Fatal("SSE", stream.StatusCode, string(body))
	}
	scanner := bufio.NewScanner(stream.Body)
	found := false
	for scanner.Scan() {
		if bytes.HasPrefix(scanner.Bytes(), []byte("data:")) {
			found = true
			break
		}
	}
	stream.Body.Close()
	cancel()
	if !found {
		t.Fatal("missing SSE snapshot", scanner.Err())
	}
	service.heartbeatStarted = make(chan struct{})
	service.heartbeatRelease = make(chan struct{})
	heartbeatResult := make(chan int, 1)
	go func() { code, _ := call(0, "POST", path+"/heartbeat", "", nil); heartbeatResult <- code }()
	select {
	case <-service.heartbeatStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not start")
	}
	service.mu.Lock()
	service.closeFails = true
	service.mu.Unlock()
	if code, _ := call(0, "DELETE", path, "", nil); code != 503 {
		t.Fatal("close failure must be visible", code)
	}
	close(service.heartbeatRelease)
	if code := <-heartbeatResult; code != 410 {
		t.Fatal("late heartbeat must not reopen", code)
	}
	if code, _ := call(0, "GET", path, "", nil); code != 410 {
		t.Fatal("closed state", code)
	}
	service.mu.Lock()
	service.closeFails = false
	service.mu.Unlock()
	if code, _ := call(0, "DELETE", path, "", nil); code != 204 {
		t.Fatal("close retry", code)
	}
	if code, _ := call(0, "POST", path+"/resume", "", nil); code != 410 {
		t.Fatal("closed resume", code)
	}
	if code, _ := call(0, "POST", "/browser-sessions", "same-session", sessionInput); code != 409 {
		t.Fatal("terminal replay", code)
	}
	if python := os.Getenv("SCRAPIO_V22_UI_PYTHON"); python != "" {
		service.heartbeatStarted = nil
		service.heartbeatRelease = nil
		service.mu.Lock()
		service.closeFails = true
		service.mu.Unlock()
		smokeCtx, stop := context.WithTimeout(ctx, 90*time.Second)
		defer stop()
		script := filepath.Join(filepath.Dir(source), "..", "..", "scripts", "tests", "v22_ui_smoke.py")
		smoke := exec.CommandContext(smokeCtx, python, script)
		input, _ := json.Marshal(map[string]string{"base": server.URL, "token": tokens[0], "collector_id": copyID})
		smoke.Stdin = bytes.NewReader(input)
		smoke.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
		smoke.Stdout = os.Stdout
		smoke.Stderr = os.Stderr
		if err := smoke.Run(); err != nil {
			t.Fatalf("UI smoke: %v", err)
		}
	}
	// No writes from this test reach public or legacy records.
}
