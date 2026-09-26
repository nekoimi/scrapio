package record_repo

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
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
	"github.com/nekoimi/scrapio/internal/repo/plugin_repo"
	"github.com/spf13/viper"
)

func TestDevD02SubscriptionDelivery(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_D02") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_D02=1 to test dev PostgreSQL")
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
	var name string
	if err := probe.QueryRow("SELECT current_database()").Scan(&name); err != nil || name != "get_magnet_dev" {
		t.Fatal("unexpected dev database")
	}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	life := db.NewDBLifecycle()
	if err := life.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer life.Stop(ctx)
	db.Instance().ShowSQL(false)
	project, err := dataset_repo.CreateProject(fmt.Sprintf("d02_%d", time.Now().UnixNano()), "D02 fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := dataset_repo.CreateDataset(dataset_repo.CreateDatasetInput{ProjectID: project.Id, Code: "items", Name: "D02 records", RecordType: "article", SchemaInput: dataset_repo.SchemaInput{UniqueKeyFields: []string{"url"}, EmptyValuePolicy: "preserve", Fields: []dataset_repo.FieldInput{{Key: "url", Label: "URL", Type: "url", Required: true}, {Key: "title", Label: "Title", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var workflowID, versionID, runID, taskID, sourceID int64
	defer func() {
		statements := []struct {
			q    string
			args []any
		}{
			{"DELETE FROM plugin_tasks WHERE dataset_id=$1", []any{dataset.Id}},
			{"DELETE FROM plugin_subscriptions WHERE dataset_id=$1", []any{dataset.Id}},
			{"DELETE FROM record_revisions WHERE record_id IN (SELECT id FROM records WHERE dataset_id=$1)", []any{dataset.Id}},
			{"DELETE FROM record_observations WHERE dataset_id=$1", []any{dataset.Id}},
			{"DELETE FROM records WHERE dataset_id=$1", []any{dataset.Id}},
			{"DELETE FROM crawl_tasks WHERE run_id=$1", []any{runID}},
			{"DELETE FROM workflow_runs WHERE id=$1", []any{runID}},
			{"DELETE FROM workflow_versions WHERE id=$1", []any{versionID}},
			{"DELETE FROM workflows WHERE id=$1", []any{workflowID}},
			{"DELETE FROM datasets WHERE id=$1", []any{dataset.Id}},
			{"DELETE FROM projects WHERE id=$1", []any{project.Id}},
		}
		for _, item := range statements {
			if _, err := probe.Exec(item.q, item.args...); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := probe.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflows(source_id,project_id,dataset_id,code,name,resource_type) VALUES($1,$2,$3,$4,'D02','article') RETURNING id", sourceID, project.Id, dataset.Id, fmt.Sprintf("d02_%d", time.Now().UnixNano())).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_versions(workflow_id,version,status,definition) VALUES($1,1,'published','{}') RETURNING id", workflowID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_runs(workflow_id,workflow_version_id,trigger_type,status) VALUES($1,$2,'manual','running') RETURNING id", workflowID, versionID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO crawl_tasks(run_id,step_name,task_type,status) VALUES($1,'trigger','fetch','running') RETURNING id", runID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	sub, err := plugin_repo.SaveSubscription(plugin_repo.SubscriptionInput{DatasetID: dataset.Id, WorkflowID: &workflowID, PluginCode: "aria2", EventType: "record.created", URLField: "url", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{DatasetID: dataset.Id, WorkflowID: &workflowID, WorkflowVersionID: &versionID, RunID: &runID, TaskID: &taskID, Values: map[string]any{"url": "https://example.org/d02", "title": "First"}, IdempotencyKey: "d02:first"}
	first, err := Save(candidate)
	if err != nil || first.Decision != "created" {
		t.Fatalf("save: %+v %v", first, err)
	}
	assertCount := func(want int) {
		t.Helper()
		var count int
		if err := probe.QueryRow("SELECT count(*) FROM plugin_tasks WHERE subscription_id=$1", sub.Id).Scan(&count); err != nil || count != want {
			t.Fatalf("task count %d want %d: %v", count, want, err)
		}
	}
	assertCount(1)
	queued, found, err := plugin_repo.ListFiltered(plugin_repo.ListFilter{DatasetID: dataset.Id, WorkflowID: workflowID, Page: 1, Size: 10})
	if err != nil || found != 1 || len(queued) != 1 || queued[0].RecordId == nil || queued[0].ObservationId == nil {
		t.Fatalf("record plugin task was not visible in monitoring: %d %+v %v", found, queued, err)
	}
	var url, status string
	if err := probe.QueryRow("SELECT input->>'url',status FROM plugin_tasks WHERE subscription_id=$1", sub.Id).Scan(&url, &status); err != nil || url != "https://example.org/d02" || status != "queued" {
		t.Fatalf("queued snapshot: %s %s %v", url, status, err)
	}
	if _, err := Save(candidate); err != nil {
		t.Fatal(err)
	}
	assertCount(1)
	candidate.IdempotencyKey = "d02:second"
	candidate.Values["title"] = "Second"
	if _, err := Save(candidate); err != nil {
		t.Fatal(err)
	}
	assertCount(1)
	if _, err := plugin_repo.SaveSubscription(plugin_repo.SubscriptionInput{DatasetID: dataset.Id, WorkflowID: &workflowID, PluginCode: "aria2", EventType: "record.updated", URLField: "url", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	candidate.IdempotencyKey = "d02:third"
	candidate.Values["title"] = "Third"
	if _, err := Save(candidate); err != nil {
		t.Fatal(err)
	}
	var updated int
	if err := probe.QueryRow("SELECT count(*) FROM plugin_tasks WHERE dataset_id=$1", dataset.Id).Scan(&updated); err != nil || updated != 2 {
		t.Fatalf("updated tasks %d: %v", updated, err)
	}
	candidate.IdempotencyKey = "d02:replay"
	candidate.Values["title"] = "Replay"
	candidate.SuppressPlugins = true
	if _, err := Save(candidate); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("SELECT count(*) FROM plugin_tasks WHERE dataset_id=$1", dataset.Id).Scan(&updated); err != nil || updated != 2 {
		t.Fatalf("replay tasks %d: %v", updated, err)
	}
	// A later candidate failure rolls back both the record and its queued event.
	_, err = SaveBatch([]Candidate{{DatasetID: dataset.Id, WorkflowID: &workflowID, WorkflowVersionID: &versionID, RunID: &runID, TaskID: &taskID, Values: map[string]any{"url": "https://example.org/d02/batch", "title": "Batch"}}, {DatasetID: dataset.Id, Values: map[string]any{"title": "invalid without unique URL"}}})
	if err == nil {
		t.Fatal("invalid batch unexpectedly committed")
	}
	var count int
	if err := probe.QueryRow("SELECT count(*) FROM records WHERE dataset_id=$1 AND normalized->>'url'='https://example.org/d02/batch'", dataset.Id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("batch record survived rollback: %d %v", count, err)
	}
	if err := probe.QueryRow("SELECT count(*) FROM plugin_tasks WHERE dataset_id=$1", dataset.Id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("batch plugin task survived rollback: %d %v", count, err)
	}
	// Disabled subscriptions do not enqueue even when a matching change commits.
	if _, err := plugin_repo.SaveSubscription(plugin_repo.SubscriptionInput{ID: sub.Id, DatasetID: dataset.Id, WorkflowID: &workflowID, PluginCode: "aria2", EventType: "record.created", URLField: "url", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(Candidate{DatasetID: dataset.Id, WorkflowID: &workflowID, WorkflowVersionID: &versionID, RunID: &runID, TaskID: &taskID, Values: map[string]any{"url": "https://example.org/d02/disabled"}}); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("SELECT count(*) FROM plugin_tasks WHERE dataset_id=$1", dataset.Id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("disabled subscription queued task: %d %v", count, err)
	}
}
