package task_repo

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/spf13/viper"
)

func TestDevRunInspection(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_B05") != "1" && os.Getenv("SCRAPIO_TEST_DEV_C04") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_C04=1 to test development PostgreSQL")
	}
	v := viper.New()
	v.SetConfigFile("../../../config/dev.yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	probe, err := sql.Open("postgres", v.GetString("db.dsn"))
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	var database string
	if err = probe.QueryRow("SELECT current_database()").Scan(&database); err != nil || database != "get_magnet_dev" {
		t.Fatalf("unexpected database %q: %v", database, err)
	}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	life := db.NewDBLifecycle()
	if err = life.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer life.Stop(ctx)
	db.Instance().ShowSQL(false)

	var sourceID, projectID, datasetID, workflowID, versionID, runID, recordID, documentID int64
	if err = probe.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err = probe.QueryRow("SELECT id,project_id FROM datasets WHERE project_id IS NOT NULL ORDER BY id LIMIT 1").Scan(&datasetID, &projectID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, item := range []struct {
			query string
			id    int64
		}{
			{"DELETE FROM record_observations WHERE run_id=$1", runID},
			{"DELETE FROM records WHERE id=$1", recordID},
			{"DELETE FROM workflow_runs WHERE id=$1", runID},
			{"DELETE FROM documents WHERE id=$1", documentID},
			{"DELETE FROM workflows WHERE id=$1", workflowID},
		} {
			if item.id > 0 {
				if _, e := probe.Exec(item.query, item.id); e != nil {
					t.Error(e)
				}
			}
		}
	}()
	code := fmt.Sprintf("b05_%d", time.Now().UnixNano())
	if err = probe.QueryRow("INSERT INTO workflows(source_id,project_id,dataset_id,code,name,resource_type) VALUES($1,$2,$3,$4,'B05 inspection test','article') RETURNING id", sourceID, projectID, datasetID, code).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err = probe.QueryRow("INSERT INTO workflow_versions(workflow_id,version,status) VALUES($1,1,'draft') RETURNING id", workflowID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = probe.QueryRow("INSERT INTO workflow_runs(workflow_id,workflow_version_id,trigger_type,status) VALUES($1,$2,'manual','partial') RETURNING id", workflowID, versionID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	insertTask := func(status string) int64 {
		t.Helper()
		var id int64
		if e := probe.QueryRow("INSERT INTO crawl_tasks(run_id,step_name,task_type,status,input) VALUES($1,'detail','workflow',$2,'{\"url\":\"https://example.org/b05\"}') RETURNING id", runID, status).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	success := insertTask("succeeded")
	failed := insertTask("dead_letter")
	limited := insertTask("limited")
	if err = probe.QueryRow("INSERT INTO documents(task_id,document_type,content) VALUES($1,'html','<h1>B05</h1>') RETURNING id", success).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec("UPDATE documents SET created_at=NOW()-INTERVAL '100 days' WHERE id=$1", documentID); err != nil {
		t.Fatal(err)
	}
	orphanID := int64(0)
	if err := probe.QueryRow("INSERT INTO documents(document_type,content,content_size,created_at) VALUES('html','old orphan',10,NOW()-INTERVAL '100 days') RETURNING id").Scan(&orphanID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = probe.Exec("DELETE FROM documents WHERE id=$1", orphanID) }()
	if _, err = probe.Exec("UPDATE crawl_tasks SET output_document_id=$1 WHERE id=$2", documentID, success); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SCRAPIO_TEST_DEV_C04") == "1" {
		cutoff := time.Now().AddDate(0, 0, -90)
		preview, err := PreviewDocumentRetention(cutoff)
		if err != nil || preview.Eligible < 1 || preview.Protected < 1 {
			t.Fatalf("retention preview: %+v %v", preview, err)
		}
		if _, err := cleanupExpiredDocuments(cutoff, orphanID); err != nil {
			t.Fatal(err)
		}
		var exists bool
		if err := probe.QueryRow("SELECT EXISTS(SELECT 1 FROM documents WHERE id=$1)", orphanID).Scan(&exists); err != nil || exists {
			t.Fatalf("orphan still exists: %v %v", exists, err)
		}
		if err := probe.QueryRow("SELECT EXISTS(SELECT 1 FROM documents WHERE id=$1)", documentID).Scan(&exists); err != nil || !exists {
			t.Fatalf("referenced document deleted: %v %v", exists, err)
		}
	}
	for _, a := range []struct {
		task             int64
		number           int
		status, snapshot string
	}{
		{success, 1, "failed", `{"stage":"fetch","candidate_count":9}`},
		{success, 2, "succeeded", `{"discovered_count":3,"record_count":2,"stop_reason":"max_pages"}`},
		{failed, 1, "failed", `{"stage":"persist","candidate_count":1}`},
	} {
		if _, err = probe.Exec("INSERT INTO task_attempts(task_id,attempt_no,status,response_snapshot) VALUES($1,$2,$3,$4)", a.task, a.number, a.status, a.snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = probe.Exec("INSERT INTO run_limit_events(run_id,task_id,page_role,url,reason) VALUES($1,$2,'detail','https://example.org/b05','max_tasks')", runID, limited); err != nil {
		t.Fatal(err)
	}
	if err = probe.QueryRow("INSERT INTO records(dataset_id,canonical_key,normalized,content_hash) VALUES($1,$2,'{}',$3) RETURNING id", datasetID, code, fmt.Sprintf("%064d", 1)).Scan(&recordID); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"created", "updated"} {
		if _, err = probe.Exec("INSERT INTO record_observations(record_id,dataset_id,schema_version,run_id,task_id,raw_fields,normalized_fields,decision) VALUES($1,$2,1,$3,$4,'{}','{}',$5)", recordID, datasetID, runID, success, decision); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 102; i++ {
		insertTask("queued")
	}

	inspection, err := InspectRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Tasks != 105 || inspection.PagesVisited != 1 || inspection.PagesFailed != 1 || inspection.PagesLimited != 1 || inspection.Discovered != 3 || inspection.Candidates != 3 || inspection.Created != 1 || inspection.Updated != 1 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
	if len(inspection.StopReasons) != 2 || inspection.StopReasons[0] != "max_pages" || inspection.StopReasons[1] != "max_tasks" {
		t.Fatalf("stop reasons: %v", inspection.StopReasons)
	}
	first, total, err := ListRunTasks(runID, 1, 100)
	if err != nil || total != 105 || len(first) != 100 {
		t.Fatalf("first page: %d %d %v", len(first), total, err)
	}
	second, total, err := ListRunTasks(runID, 2, 100)
	if err != nil || total != 105 || len(second) != 5 || first[99].Id >= second[0].Id {
		t.Fatalf("second page: %d %d %v", len(second), total, err)
	}
	attempts, total, err := ListTaskAttempts(success, 1, 20)
	if err != nil || total != 2 || len(attempts) != 2 || attempts[0].AttemptNo != 2 {
		t.Fatalf("attempts: %+v %d %v", attempts, total, err)
	}
}
