package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/crawlpolicy"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
	"github.com/nekoimi/scrapio/internal/repo/plugin_repo"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func TestDevA04Templates(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_A04") != "1" && os.Getenv("SCRAPIO_TEST_DEV_D01") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_D01=1 to test config/dev.yaml PostgreSQL")
	}
	previousLogLevel := log.GetLevel()
	defer log.SetLevel(previousLogLevel)
	log.SetLevel(log.ErrorLevel)
	v := viper.New()
	v.SetConfigFile("../../config/dev.yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatal("cannot read dev config")
	}
	probe, err := sql.Open("postgres", v.GetString("db.dsn"))
	if err != nil {
		t.Fatal("cannot open development database")
	}
	var databaseName string
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probeErr := probe.QueryRowContext(probeCtx, "SELECT current_database()").Scan(&databaseName)
	probe.Close()
	if probeErr != nil || databaseName != "get_magnet_dev" {
		t.Fatal("expected get_magnet_dev before initializing migrations")
	}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	lifecycle := db.NewDBLifecycle()
	if err := lifecycle.Start(ctx); err != nil {
		t.Fatal("cannot initialize database")
	}
	defer lifecycle.Stop(ctx)
	db.Instance().ShowSQL(false)
	raw := db.Instance().DB().DB
	var name string
	if err := raw.QueryRow("SELECT current_database()").Scan(&name); err != nil || name != "get_magnet_dev" {
		t.Fatal("unexpected database")
	}
	project, err := dataset_repo.CreateProject(fmt.Sprintf("a04_%d", time.Now().UnixNano()), "A04 integration", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		statements := []string{
			`DELETE FROM plugin_tasks WHERE dataset_id IN (SELECT id FROM datasets WHERE project_id=$1)`,
			`DELETE FROM plugin_subscriptions WHERE dataset_id IN (SELECT id FROM datasets WHERE project_id=$1)`,
			`DELETE FROM record_revisions WHERE record_id IN (SELECT r.id FROM records r JOIN datasets d ON d.id=r.dataset_id WHERE d.project_id=$1)`,
			`DELETE FROM record_observations WHERE dataset_id IN (SELECT id FROM datasets WHERE project_id=$1)`,
			`DELETE FROM records WHERE dataset_id IN (SELECT id FROM datasets WHERE project_id=$1)`,
			`DELETE FROM documents WHERE task_id IN (SELECT t.id FROM crawl_tasks t JOIN workflow_runs r ON r.id=t.run_id JOIN workflows w ON w.id=r.workflow_id WHERE w.project_id=$1)`,
			`DELETE FROM workflow_runs WHERE workflow_id IN (SELECT id FROM workflows WHERE project_id=$1)`,
			`DELETE FROM workflows WHERE project_id=$1`,
			`DELETE FROM datasets WHERE project_id=$1`,
		}
		for _, statement := range statements {
			if _, err := raw.Exec(statement, project.Id); err != nil {
				t.Error("cleanup:", err)
			}
		}
		if _, err := raw.Exec("DELETE FROM projects WHERE id=$1", project.Id); err != nil {
			t.Error(err)
		}
	}()
	dataset, err := dataset_repo.CreateDataset(dataset_repo.CreateDatasetInput{ProjectID: project.Id, Code: "article", Name: "A04 test", RecordType: "article", SchemaInput: dataset_repo.SchemaInput{UniqueKeyFields: []string{"url"}, EmptyValuePolicy: "preserve", Fields: []dataset_repo.FieldInput{{Key: "url", Label: "URL", Type: "url", Required: true}, {Key: "title", Label: "Title", Type: "string"}, {Key: "body", Label: "Body", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	magnetDataset, err := dataset_repo.CreateDataset(dataset_repo.CreateDatasetInput{ProjectID: project.Id, Code: "magnet", Name: "Magnet test", RecordType: "magnet", SchemaInput: dataset_repo.SchemaInput{UniqueKeyFields: []string{"canonical_key"}, EmptyValuePolicy: "preserve", Fields: []dataset_repo.FieldInput{{Key: "canonical_key", Label: "Key", Type: "string", Required: true}, {Key: "number", Label: "Number", Type: "string"}, {Key: "title", Label: "Title", Type: "string"}, {Key: "links", Label: "Links", Type: "string", Multiple: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	var sourceID int64
	if err := raw.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			w.Header().Set("Content-Type", "text/html")
			switch r.URL.Query().Get("page") {
			case "1":
				fmt.Fprint(w, `<a class="item" href="/detail/a">A</a><a class="item" href="/detail/a#fragment">duplicate</a><a class="item" href="/detail/b">B</a><a class="item" href="https://offsite.invalid/detail">offsite</a><a class="next" href="?page=2">Next</a>`)
			case "2":
				fmt.Fprint(w, `<a class="item" href="/detail/b">repeated</a><a class="item" href="/detail/c">C</a><a class="next" href="?page=3">Next</a>`)
			default:
				fmt.Fprint(w, `<a class="next" href="?page=2">Repeated next</a>`)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/detail/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<link rel="canonical" href="%s%s"><h1>%s</h1><article>Body</article>`, "http://"+r.Host, r.URL.Path, r.URL.Path)
			return
		}
		if r.URL.Path == "/magnet" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<span class="number">AB-123</span><h1>Magnet title</h1><a href="magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA">one</a><a href="magnet:?xt=urn:btih:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB">two</a>`)
			return
		}
		if r.URL.Path == "/page" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<link rel="canonical" href="https://a04-test.invalid/a"><h1>Old title</h1><article>Body</article>`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":[{"url":"https://a04-test.invalid/a","title":"New title","body":"Body"},{"url":"https://a04-test.invalid/b","title":"Second","body":"Text"}]}`)
	}))
	defer server.Close()
	worker := NewWorker()
	execute := func(template Template, code, path string, target *table.Dataset) int64 {
		t.Helper()
		template.Definition.Trigger.URL = server.URL + path
		encoded, _ := json.Marshal(template.Definition)
		now := time.Now()
		workflow := &table.Workflow{ProjectId: &project.Id, DatasetId: &target.Id, SourceId: sourceID, Code: code, Name: code, ResourceType: target.RecordType, Enabled: true, CreatedAt: now, UpdatedAt: now}
		if _, err := db.Instance().InsertOne(workflow); err != nil {
			t.Fatal(err)
		}
		version := &table.WorkflowVersion{WorkflowId: workflow.Id, Version: 1, Status: "published", Definition: string(encoded), CreatedAt: now}
		if _, err := db.Instance().InsertOne(version); err != nil {
			t.Fatal(err)
		}
		run := &table.WorkflowRun{WorkflowId: workflow.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: "running", Input: "{}", Summary: "{}", CreatedAt: now}
		if _, err := db.Instance().InsertOne(run); err != nil {
			t.Fatal(err)
		}
		lease := now.Add(time.Minute)
		task := table.CrawlTask{RunId: run.Id, StepName: "trigger", TaskType: "workflow", Input: "{}", Status: "running", AttemptCount: 1, MaxAttempts: 1, LeaseOwner: "a04-test", LeaseUntil: &lease, CreatedAt: now, UpdatedAt: now}
		if _, err := db.Instance().InsertOne(&task); err != nil {
			t.Fatal(err)
		}
		attempt := table.TaskAttempt{TaskId: task.Id, AttemptNo: 1, WorkerId: "a04-test", Status: "running", StartedAt: &now, RequestSnapshot: "{}", ResponseSnapshot: "{}"}
		if _, err := db.Instance().InsertOne(&attempt); err != nil {
			t.Fatal(err)
		}
		if err := worker.execute(ctx, &task_repo.Claim{Task: task, Attempt: attempt}); err != nil {
			t.Fatal("template execution:", err)
		}
		var status string
		if err := raw.QueryRow("SELECT status FROM workflow_runs WHERE id=$1", run.Id).Scan(&status); err != nil || status != "succeeded" {
			t.Fatalf("run status=%s err=%v", status, err)
		}
		return run.Id
	}
	templates := Templates()
	execute(templates[0], "page", "/page", dataset)
	jsonRun := execute(templates[1], "json", "/items", dataset)
	execute(templates[1], "json_retry", "/items", dataset)
	magnet := templates[0]
	magnet.Definition.Nodes = []Node{{Name: "extract", Type: "extract", Config: map[string]any{"content_type": "html", "fields": []any{
		map[string]any{"name": "canonical_key", "selector": ".number", "required": true},
		map[string]any{"name": "number", "selector": ".number"},
		map[string]any{"name": "title", "selector": "h1"},
		map[string]any{"name": "links", "selector": "a[href^=\"magnet:\"]", "attribute": "href", "multiple": true},
	}}}}
	magnetRun := execute(magnet, "magnet", "/magnet", magnetDataset)
	var magnetKey, magnetValues string
	if err := raw.QueryRow("SELECT canonical_key,normalized::text FROM records WHERE dataset_id=$1", magnetDataset.Id).Scan(&magnetKey, &magnetValues); err != nil {
		t.Fatal("magnet record:", err)
	}
	if magnetKey != "AB-123" || !strings.Contains(magnetValues, "magnet:?xt=urn:btih:AAAAAAAA") || !strings.Contains(magnetValues, "magnet:?xt=urn:btih:BBBBBBBB") {
		t.Fatalf("magnet values: key=%q values=%s", magnetKey, magnetValues)
	}
	var magnetObservations int
	if err := raw.QueryRow("SELECT count(*) FROM record_observations WHERE dataset_id=$1 AND run_id=$2 AND document_id IS NOT NULL", magnetDataset.Id, magnetRun).Scan(&magnetObservations); err != nil || magnetObservations != 1 {
		t.Fatalf("magnet provenance: observations=%d err=%v", magnetObservations, err)
	}
	var records, observations, revisions int
	if err := raw.QueryRow("SELECT count(*) FROM records WHERE dataset_id=$1", dataset.Id).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM record_observations WHERE dataset_id=$1", dataset.Id).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM record_revisions v JOIN records r ON r.id=v.record_id WHERE r.dataset_id=$1", dataset.Id).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if records != 2 || observations != 5 || revisions != 3 {
		t.Fatalf("records=%d observations=%d revisions=%d", records, observations, revisions)
	}
	var recordID, observedRun, documentID, versionID, taskID int64
	if err := raw.QueryRow(`SELECT rec.id,ob.run_id,ob.document_id,ob.workflow_version_id,ob.task_id
		FROM records rec JOIN record_observations ob ON ob.record_id=rec.id
		WHERE rec.dataset_id=$1 AND ob.run_id=$2 ORDER BY ob.id LIMIT 1`, dataset.Id, jsonRun).Scan(&recordID, &observedRun, &documentID, &versionID, &taskID); err != nil {
		t.Fatal("record provenance:", err)
	}
	var storedType string
	if err := raw.QueryRow("SELECT document_type FROM documents WHERE id=$1 AND task_id=$2", documentID, taskID).Scan(&storedType); err != nil || storedType != "json" || observedRun != jsonRun || versionID == 0 || recordID == 0 {
		t.Fatalf("record provenance mismatch: record=%d run=%d document=%d version=%d type=%q error=%v", recordID, observedRun, documentID, versionID, storedType, err)
	}
	var summary string
	if err := raw.QueryRow("SELECT summary::text FROM workflow_runs WHERE id=$1", jsonRun).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	var counts task_repo.RunSummary
	if err := json.Unmarshal([]byte(summary), &counts); err != nil || counts.Created != 1 || counts.Updated != 1 {
		t.Fatalf("summary=%s %v", summary, err)
	}
	_, err = record_repo.SaveBatch([]record_repo.Candidate{{DatasetID: dataset.Id, Values: map[string]any{"url": "https://a04-test.invalid/rollback", "title": "Good"}}, {DatasetID: dataset.Id, Values: map[string]any{"title": "Missing key"}}})
	if err == nil {
		t.Fatal("invalid batch accepted")
	}
	var rollbackCount int
	if err := raw.QueryRow("SELECT count(*) FROM records WHERE dataset_id=$1 AND canonical_key='https://a04-test.invalid/rollback'", dataset.Id).Scan(&rollbackCount); err != nil || rollbackCount != 0 {
		t.Fatal("batch did not rollback")
	}
	listing := Templates()[3].Definition
	listing.Trigger.URL = server.URL + "/list?page=1"
	listing.Listing.MaxPages = 5
	listing.Listing.DetailSelector = "a.item[href]"
	listing.Listing.NextSelector = "a.next[href]"
	encoded, _ := json.Marshal(listing)
	now := time.Now()
	owner := &table.Workflow{ProjectId: &project.Id, DatasetId: &dataset.Id, SourceId: sourceID, Code: "listing", Name: "listing", ResourceType: "article", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if _, err := db.Instance().InsertOne(owner); err != nil {
		t.Fatal(err)
	}
	version := &table.WorkflowVersion{WorkflowId: owner.Id, Version: 1, Status: "published", Definition: string(encoded), CreatedAt: now}
	if _, err := db.Instance().InsertOne(version); err != nil {
		t.Fatal(err)
	}
	run := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: "running", Input: "{}", Summary: "{}", CreatedAt: now}
	if _, err := db.Instance().InsertOne(run); err != nil {
		t.Fatal(err)
	}
	root, err := task_repo.CreateTask(run.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	drain := func(runID int64) {
		for i := 0; i < 12; i++ {
			var task table.CrawlTask
			has, err := db.Instance().Where("run_id=? AND status=?", runID, task_repo.TaskQueued).Asc("id").Get(&task)
			if err != nil {
				t.Fatal(err)
			}
			if !has {
				break
			}
			lease := time.Now().Add(time.Minute)
			if _, err := db.Instance().ID(task.Id).Cols("status", "attempt_count", "lease_owner", "lease_until").Update(&table.CrawlTask{Status: task_repo.TaskRunning, AttemptCount: 1, LeaseOwner: "b01-test", LeaseUntil: &lease}); err != nil {
				t.Fatal(err)
			}
			task.Status, task.AttemptCount, task.LeaseOwner, task.LeaseUntil = task_repo.TaskRunning, 1, "b01-test", &lease
			attempt := table.TaskAttempt{TaskId: task.Id, AttemptNo: 1, WorkerId: "b01-test", Status: task_repo.TaskRunning, StartedAt: &now, RequestSnapshot: task.Input, ResponseSnapshot: "{}"}
			if _, err := db.Instance().InsertOne(&attempt); err != nil {
				t.Fatal(err)
			}
			if err := worker.execute(ctx, &task_repo.Claim{Task: task, Attempt: attempt}); err != nil {
				t.Fatalf("listing task %s #%d: %v", task.StepName, task.Id, err)
			}
		}
	}
	drain(run.Id)
	var queued, listTasks, detailTasks, distinctURLs int
	if err := raw.QueryRow(`SELECT count(*) FILTER (WHERE status='queued'),count(*) FILTER (WHERE step_name IN ('trigger','list')),count(*) FILTER (WHERE step_name='detail'),count(DISTINCT input->>'url') FILTER (WHERE step_name='detail') FROM crawl_tasks WHERE run_id=$1`, run.Id).Scan(&queued, &listTasks, &detailTasks, &distinctURLs); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || listTasks != 3 || detailTasks != 3 || distinctURLs != 3 || root.Id == 0 {
		t.Fatalf("listing task tree: queued=%d list=%d details=%d distinct=%d", queued, listTasks, detailTasks, distinctURLs)
	}
	var listingRecords, listingObservations int
	if err := raw.QueryRow(`SELECT count(*) FROM records WHERE dataset_id=$1 AND canonical_key LIKE $2`, dataset.Id, server.URL+"/detail/%").Scan(&listingRecords); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM record_observations WHERE run_id=$1 AND document_id IS NOT NULL", run.Id).Scan(&listingObservations); err != nil {
		t.Fatal(err)
	}
	var listingStatus string
	if err := raw.QueryRow("SELECT status FROM workflow_runs WHERE id=$1", run.Id).Scan(&listingStatus); err != nil {
		t.Fatal(err)
	}
	if listingRecords != 3 || listingObservations != 3 || listingStatus != task_repo.RunSucceeded {
		t.Fatalf("listing persistence: records=%d observations=%d status=%s", listingRecords, listingObservations, listingStatus)
	}
	limitedRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunRunning, Input: "{}", Summary: "{}", CreatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(limitedRun); err != nil {
		t.Fatal(err)
	}
	limit := crawlpolicy.Defaults(listing.Trigger.URL)
	limit.MaxDiscoveredPerPage, limit.MaxTasks, limit.MaxPages = 1, 4, 4
	budgetJSON, _ := json.Marshal(limit)
	if _, err := raw.Exec("UPDATE workflow_runs SET budget=$1::jsonb WHERE id=$2", string(budgetJSON), limitedRun.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := task_repo.CreateTask(limitedRun.Id, 0, "trigger", workflowTaskType, "{}", 5); err != nil {
		t.Fatal(err)
	}
	drain(limitedRun.Id)
	var limitedStatus string
	if err := raw.QueryRow("SELECT status FROM workflow_runs WHERE id=$1", limitedRun.Id).Scan(&limitedStatus); err != nil {
		t.Fatal(err)
	}
	coverage, err := task_repo.RunCoverage(limitedRun.Id)
	if err != nil {
		t.Fatal(err)
	}
	limitEvents, err := task_repo.LimitEvents(limitedRun.Id, 100)
	if err != nil || len(limitEvents) < 2 {
		t.Fatalf("limit events: %#v %v", limitEvents, err)
	}
	if limitedStatus != task_repo.RunLimited || coverage.Tasks != 4 || coverage.LimitReasons["max_discovered_per_page"] == 0 || coverage.LimitReasons["max_tasks"] == 0 || coverage.PagesReserved != 4 {
		t.Fatalf("budget coverage: status=%s coverage=%+v", limitedStatus, coverage)
	}
	var observationsLimited int
	if err := raw.QueryRow("SELECT count(*) FROM record_observations WHERE run_id=$1", limitedRun.Id).Scan(&observationsLimited); err != nil || observationsLimited != 2 {
		t.Fatalf("limited run records=%d err=%v", observationsLimited, err)
	}
	// Recover an expired lease, then reject the old attempt's write and reuse
	// the same task identity, reserved page and saved document.
	recoveryRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunRunning, Input: "{}", Summary: "{}", CreatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(recoveryRun); err != nil {
		t.Fatal(err)
	}
	recoveryTask, err := task_repo.CreateTask(recoveryRun.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	var replayDocumentID int64
	replayContent := `<a class="item" href="/detail/a">A</a><a class="next" href="?page=2">Next</a>`
	if err := raw.QueryRow("INSERT INTO documents(task_id,document_type,content,content_size,metadata) VALUES($1,'html',$2,$3,$4::jsonb) RETURNING id", recoveryTask.Id, replayContent, len(replayContent), fmt.Sprintf(`{"final_url":%q}`, listing.Trigger.URL)).Scan(&replayDocumentID); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE crawl_tasks SET output_document_id=$1 WHERE id=$2", replayDocumentID, recoveryTask.Id); err != nil {
		t.Fatal(err)
	}
	oldLease := time.Now().Add(-time.Minute)
	if _, err := raw.Exec("UPDATE crawl_tasks SET status='running',attempt_count=1,lease_owner='dead-worker',lease_until=$1 WHERE id=$2", oldLease, recoveryTask.Id); err != nil {
		t.Fatal(err)
	}
	oldAttempt := &table.TaskAttempt{TaskId: recoveryTask.Id, AttemptNo: 1, WorkerId: "dead-worker", Status: task_repo.TaskRunning, StartedAt: &now, RequestSnapshot: "{}", ResponseSnapshot: "{}"}
	if _, err := db.Instance().InsertOne(oldAttempt); err != nil {
		t.Fatal(err)
	}
	if count, err := task_repo.RecoverExpiredLeases(); err != nil || count < 1 {
		t.Fatalf("lease recovery: count=%d err=%v", count, err)
	}
	if err := task_repo.CheckAttempt(recoveryTask.Id, *oldAttempt); err == nil {
		t.Fatal("expired attempt remains writable")
	}
	claimed, found, err := task_repo.ClaimNextType("replacement-worker", time.Minute, workflowTaskType)
	if err != nil || !found || claimed.Task.Id != recoveryTask.Id || claimed.Attempt.AttemptNo != 2 {
		t.Fatalf("reclaim: %#v found=%t err=%v", claimed, found, err)
	}
	if _, err := task_repo.CreateTaskForAttempt(&task_repo.Claim{Task: *recoveryTask, Attempt: *oldAttempt}, "detail", task_repo.TaskInput(server.URL+"/detail/stale", "")); err == nil {
		t.Fatal("stale attempt created a detail task")
	}
	if err := worker.execute(ctx, claimed); err != nil {
		t.Fatal("replayed task:", err)
	}
	var replayDocumentCount int
	if err := raw.QueryRow("SELECT count(*) FROM documents WHERE task_id=$1", recoveryTask.Id).Scan(&replayDocumentCount); err != nil || replayDocumentCount != 1 {
		t.Fatalf("replay fetched a second document: %d %v", replayDocumentCount, err)
	}
	drain(recoveryRun.Id)
	if err := task_repo.CancelRun(recoveryRun.Id); err == nil {
		t.Fatal("finished run was cancelled")
	}
	cancelRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunQueued, Input: "{}", Summary: "{}", CreatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(cancelRun); err != nil {
		t.Fatal(err)
	}
	cancelTask, err := task_repo.CreateTask(cancelRun.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := task_repo.CancelRun(cancelRun.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := task_repo.CreateTask(cancelRun.Id, cancelTask.Id, "detail", workflowTaskType, task_repo.TaskInput(server.URL+"/detail/z", ""), 5); err == nil {
		t.Fatal("cancelled run accepted child task")
	}
	cancelTreeRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunRunning, Input: "{}", Summary: "{}", CreatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(cancelTreeRun); err != nil {
		t.Fatal(err)
	}
	parentTask, err := task_repo.CreateTask(cancelTreeRun.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	lease := time.Now().Add(time.Minute)
	if _, err := raw.Exec("UPDATE crawl_tasks SET status='running',attempt_count=1,lease_owner='cancel-test',lease_until=$1 WHERE id=$2", lease, parentTask.Id); err != nil {
		t.Fatal(err)
	}
	childTask, err := task_repo.CreateTask(cancelTreeRun.Id, parentTask.Id, "detail", workflowTaskType, task_repo.TaskInput(server.URL+"/detail/cancel", ""), 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := task_repo.CancelTask(parentTask.Id); err != nil {
		t.Fatal(err)
	}
	var parentStatus, childStatus string
	if err := raw.QueryRow("SELECT status FROM crawl_tasks WHERE id=$1", parentTask.Id).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT status FROM crawl_tasks WHERE id=$1", childTask.Id).Scan(&childStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != task_repo.TaskCancelled || childStatus != task_repo.TaskCancelled {
		t.Fatalf("cancelled subtree: parent=%s child=%s", parentStatus, childStatus)
	}
	if err := task_repo.RetryTask(parentTask.Id); err == nil {
		t.Fatal("cancelled task was retried")
	}
	if err := task_repo.RetryTask(childTask.Id); err == nil {
		t.Fatal("cancelled child was retried")
	}
	failedRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunFailed, Input: "{}", Summary: "{}", CreatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(failedRun); err != nil {
		t.Fatal(err)
	}
	failedTask := &table.CrawlTask{RunId: failedRun.Id, StepName: "trigger", TaskType: workflowTaskType, Input: "{}", Status: task_repo.TaskDeadLetter, AttemptCount: 5, MaxAttempts: 5, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if _, err := db.Instance().InsertOne(failedTask); err != nil {
		t.Fatal(err)
	}
	if err := task_repo.RetryTask(failedTask.Id); err != nil {
		t.Fatal("retry failed task:", err)
	}
	var retryStatus string
	if err := raw.QueryRow("SELECT status FROM crawl_tasks WHERE id=$1", failedTask.Id).Scan(&retryStatus); err != nil || retryStatus != task_repo.TaskQueued {
		t.Fatalf("retry status=%s err=%v", retryStatus, err)
	}
	if err := task_repo.CancelRun(failedRun.Id); err != nil {
		t.Fatal(err)
	}
	expiredRun := &table.WorkflowRun{WorkflowId: owner.Id, WorkflowVersionId: version.Id, TriggerType: "manual", Status: task_repo.RunQueued, Input: "{}", Summary: "{}", CreatedAt: time.Now().Add(-time.Minute)}
	if _, err := db.Instance().InsertOne(expiredRun); err != nil {
		t.Fatal(err)
	}
	expiredBudget := crawlpolicy.Defaults(listing.Trigger.URL)
	expiredBudget.MaxDurationSeconds = 1
	expiredJSON, _ := json.Marshal(expiredBudget)
	if _, err := raw.Exec("UPDATE workflow_runs SET budget=$1::jsonb,created_at=$2 WHERE id=$3", string(expiredJSON), time.Now().Add(-time.Minute), expiredRun.Id); err != nil {
		t.Fatal(err)
	}
	expiredTask, err := task_repo.CreateTask(expiredRun.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := task_repo.ExpireBudgetRuns(); err != nil {
		t.Fatal(err)
	}
	var expiredStatus, expiredTaskStatus string
	if err := raw.QueryRow("SELECT status FROM workflow_runs WHERE id=$1", expiredRun.Id).Scan(&expiredStatus); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT status FROM crawl_tasks WHERE id=$1", expiredTask.Id).Scan(&expiredTaskStatus); err != nil {
		t.Fatal(err)
	}
	if expiredStatus != task_repo.RunFailed || expiredTaskStatus != task_repo.TaskLimited {
		t.Fatalf("expired empty run: run=%s task=%s", expiredStatus, expiredTaskStatus)
	}
	jsTemplate := Templates()[0]
	jsTemplate.Definition.Nodes = append(jsTemplate.Definition.Nodes, scriptNode(`return {...input,title: 'D01 transformed'};`))
	jsRun := execute(jsTemplate, "d01_script", "/page", dataset)
	var jsTitle string
	if err := raw.QueryRow("SELECT normalized->>'title' FROM records WHERE dataset_id=$1 AND canonical_key='https://a04-test.invalid/a'", dataset.Id).Scan(&jsTitle); err != nil || jsTitle != "D01 transformed" {
		t.Fatalf("JS formal record: %q %v", jsTitle, err)
	}
	var jsStatus string
	if err := raw.QueryRow("SELECT status FROM workflow_runs WHERE id=$1", jsRun).Scan(&jsStatus); err != nil || jsStatus != task_repo.RunSucceeded {
		t.Fatalf("JS run: %q %v", jsStatus, err)
	}
	// A retry can resume a saved document before its first record transaction.
	// That first observation must still enqueue the subscribed external action.
	resumeDefinition := Templates()[0].Definition
	resumeDefinition.Trigger.URL = server.URL + "/page"
	resumeEncoded, _ := json.Marshal(resumeDefinition)
	resumeOwner := &table.Workflow{ProjectId: &project.Id, DatasetId: &dataset.Id, SourceId: sourceID, Code: "saved_document", Name: "saved_document", ResourceType: "article", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if _, err := db.Instance().InsertOne(resumeOwner); err != nil {
		t.Fatal(err)
	}
	resumeVersion := &table.WorkflowVersion{WorkflowId: resumeOwner.Id, Version: 1, Status: "published", Definition: string(resumeEncoded), CreatedAt: now}
	if _, err := db.Instance().InsertOne(resumeVersion); err != nil {
		t.Fatal(err)
	}
	resumeRun := &table.WorkflowRun{WorkflowId: resumeOwner.Id, WorkflowVersionId: resumeVersion.Id, TriggerType: "manual", Status: task_repo.RunRunning, Input: "{}", Summary: "{}", CreatedAt: now}
	if _, err := db.Instance().InsertOne(resumeRun); err != nil {
		t.Fatal(err)
	}
	resumeTask, err := task_repo.CreateTask(resumeRun.Id, 0, "trigger", workflowTaskType, "{}", 5)
	if err != nil {
		t.Fatal(err)
	}
	resumeURL := server.URL + "/saved-document"
	resumeHTML := fmt.Sprintf(`<link rel="canonical" href="%s"><h1>Recovered</h1><article>Body</article>`, resumeURL)
	var savedID int64
	if err := raw.QueryRow("INSERT INTO documents(task_id,document_type,content,content_size,metadata) VALUES($1,'html',$2,$3,$4::jsonb) RETURNING id", resumeTask.Id, resumeHTML, len(resumeHTML), fmt.Sprintf(`{"final_url":%q}`, resumeDefinition.Trigger.URL)).Scan(&savedID); err != nil {
		t.Fatal(err)
	}
	resumeLease := time.Now().Add(time.Minute)
	if _, err := raw.Exec("UPDATE crawl_tasks SET output_document_id=$1,status='running',attempt_count=1,lease_owner='resume-test',lease_until=$2 WHERE id=$3", savedID, resumeLease, resumeTask.Id); err != nil {
		t.Fatal(err)
	}
	resumeTask.OutputDocumentId, resumeTask.Status, resumeTask.AttemptCount, resumeTask.LeaseOwner, resumeTask.LeaseUntil = &savedID, task_repo.TaskRunning, 1, "resume-test", &resumeLease
	resumeAttempt := table.TaskAttempt{TaskId: resumeTask.Id, AttemptNo: 1, WorkerId: "resume-test", Status: task_repo.TaskRunning, StartedAt: &now, RequestSnapshot: "{}", ResponseSnapshot: "{}"}
	if _, err := db.Instance().InsertOne(&resumeAttempt); err != nil {
		t.Fatal(err)
	}
	sub, err := plugin_repo.SaveSubscription(plugin_repo.SubscriptionInput{DatasetID: dataset.Id, WorkflowID: &resumeOwner.Id, PluginCode: "aria2", EventType: "record.created", URLField: "url", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.execute(ctx, &task_repo.Claim{Task: *resumeTask, Attempt: resumeAttempt}); err != nil {
		t.Fatal("saved document execution:", err)
	}
	var queuedPluginTasks int
	if err := raw.QueryRow("SELECT count(*) FROM plugin_tasks WHERE subscription_id=$1", sub.Id).Scan(&queuedPluginTasks); err != nil || queuedPluginTasks != 1 {
		t.Fatalf("saved document first observation queued %d plugin tasks: %v", queuedPluginTasks, err)
	}
	if _, err := record_repo.Save(record_repo.Candidate{DatasetID: dataset.Id, WorkflowID: &resumeOwner.Id, WorkflowVersionID: &resumeVersion.Id, RunID: &resumeRun.Id, TaskID: &resumeTask.Id, DocumentID: &savedID, Values: map[string]any{"url": resumeURL, "title": "Recovered", "body": "Body"}, IdempotencyKey: fmt.Sprintf("workflow-task:%d:item:0", resumeTask.Id)}); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM plugin_tasks WHERE subscription_id=$1", sub.Id).Scan(&queuedPluginTasks); err != nil || queuedPluginTasks != 1 {
		t.Fatalf("duplicate observation queued %d plugin tasks: %v", queuedPluginTasks, err)
	}
	t.Log("HTML, JSON and JS record execution, provenance, budget recovery and batch rollback passed")
}
