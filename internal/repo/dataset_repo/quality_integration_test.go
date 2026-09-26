package dataset_repo

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

func TestDevQualityTrend(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_C03") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_C03=1 for development PostgreSQL")
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
	if err := probe.QueryRow("SELECT current_database()").Scan(&database); err != nil || database != "get_magnet_dev" {
		t.Fatalf("unexpected database %q: %v", database, err)
	}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	life := db.NewDBLifecycle()
	if err := life.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer life.Stop(ctx)
	db.Instance().ShowSQL(false)
	var projectID, datasetID, sourceID, workflowID, versionID, runID, taskID int64
	if err := probe.QueryRow("SELECT project_id,id FROM datasets WHERE project_id IS NOT NULL ORDER BY id LIMIT 1").Scan(&projectID, &datasetID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if workflowID > 0 {
			_, _ = probe.Exec("DELETE FROM workflow_runs WHERE workflow_id=$1", workflowID)
			_, _ = probe.Exec("DELETE FROM workflows WHERE id=$1", workflowID)
		}
	}()
	code := fmt.Sprintf("c03_%d", time.Now().UnixNano())
	if err := probe.QueryRow("INSERT INTO workflows(project_id,dataset_id,source_id,code,name,resource_type) VALUES($1,$2,$3,$4,'C03 test','article') RETURNING id", projectID, datasetID, sourceID, code).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if first, err := Quality(workflowID); err != nil || first.HasBaseline || first.Issue != "" {
		t.Fatalf("empty baseline: %+v %v", first, err)
	}
	if err := SaveQualityThreshold(QualityThreshold{WorkflowID: workflowID, MinKeyRate: 0.8, MaxRequiredMissingRate: 0.3, MaxAnomalyRate: 0.4}); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_versions(workflow_id,version,status) VALUES($1,1,'published') RETURNING id", workflowID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_runs(workflow_id,workflow_version_id,trigger_type,status) VALUES($1,$2,'manual','failed') RETURNING id", workflowID, versionID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO crawl_tasks(run_id,step_name,task_type,status,error_message) VALUES($1,'detail','workflow','dead_letter','required field url is empty') RETURNING id", runID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	trend, err := Quality(workflowID)
	if err != nil || !trend.HasBaseline || len(trend.Runs) != 1 || trend.Runs[0].RequiredMissing != 1 || trend.Issue == "" || trend.Threshold.MinKeyRate != 0.8 {
		t.Fatalf("trend: %+v %v", trend, err)
	}
	health, err := Health(projectID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range health.Collectors {
		if c.WorkflowID == workflowID {
			found = true
			if c.Issue == "" {
				t.Fatal("missing project issue")
			}
		}
	}
	if !found {
		t.Fatal("collector absent from project health")
	}
}
