package record_repo

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
	"github.com/spf13/viper"
)

func TestDevDataConsumption(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_B04") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_B04=1 to use config/dev.yaml PostgreSQL")
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
		t.Fatalf("unexpected database %q: %v", name, err)
	}
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(ctx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	life := db.NewDBLifecycle()
	if err := life.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer life.Stop(ctx)
	db.Instance().ShowSQL(false)
	project, err := dataset_repo.CreateProject("b04_"+time.Now().Format("150405000"), "B04 integration", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var datasetID int64
	var workflowID int64
	defer func() {
		if workflowID > 0 {
			if _, err := probe.Exec("DELETE FROM workflows WHERE id=$1", workflowID); err != nil {
				t.Error(err)
			}
		}
		if datasetID > 0 {
			queries := []string{"DELETE FROM record_export_jobs WHERE dataset_id=$1", "DELETE FROM record_views WHERE dataset_id=$1", "DELETE FROM record_revisions WHERE record_id IN (SELECT id FROM records WHERE dataset_id=$1)", "DELETE FROM record_observations WHERE dataset_id=$1", "DELETE FROM records WHERE dataset_id=$1", "DELETE FROM datasets WHERE id=$1"}
			for _, q := range queries {
				if _, err := probe.Exec(q, datasetID); err != nil {
					t.Error(err)
				}
			}
		}
		if _, err := probe.Exec("DELETE FROM projects WHERE id=$1", project.Id); err != nil {
			t.Error(err)
		}
	}()
	dataset, err := dataset_repo.CreateDataset(dataset_repo.CreateDatasetInput{ProjectID: project.Id, Code: "items", Name: "B04 items", RecordType: "article", SchemaInput: dataset_repo.SchemaInput{UniqueKeyFields: []string{"title"}, EmptyValuePolicy: "preserve", Fields: []dataset_repo.FieldInput{{Key: "title", Label: "Title", Type: "string", Required: true}, {Key: "body", Label: "Body", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	datasetID = dataset.Id
	var sourceID int64
	if err := probe.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflows(source_id,project_id,dataset_id,code,name,resource_type,enabled) VALUES($1,$2,$3,$4,'B04 collector','article',true) RETURNING id", sourceID, project.Id, datasetID, "b04_"+time.Now().Format("150405000")).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	first, err := Save(Candidate{DatasetID: datasetID, SourceID: &sourceID, Values: map[string]any{"title": "=SUM(1,2)", "body": "old"}})
	if err != nil || first.Decision != "created" {
		t.Fatalf("create: %+v %v", first, err)
	}
	updated, err := Save(Candidate{DatasetID: datasetID, SourceID: &sourceID, Values: map[string]any{"title": "=SUM(1,2)", "body": "new"}})
	if err != nil || updated.Decision != "updated" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	_, err = Save(Candidate{DatasetID: datasetID, Values: map[string]any{"title": "Second", "body": "other"}})
	if err != nil {
		t.Fatal(err)
	}
	filter := Filter{DatasetID: datasetID, Query: "SUM", SourceID: sourceID, Activity: "updated"}
	rows, n, err := ListFiltered(filter, 1, 20)
	if err != nil || n != 1 || len(rows) != 1 || rows[0].Id != first.RecordID || rows[0].LastDecision != "updated" || rows[0].SourceCount != 1 {
		t.Fatalf("filter: %d %+v %v", n, rows, err)
	}
	if _, n, err := ListFiltered(Filter{DatasetID: datasetID, Query: "missing"}, 1, 20); err != nil || n != 0 {
		t.Fatalf("search: %d %v", n, err)
	}
	if _, n, err := ListFiltered(Filter{DatasetID: datasetID, Query: "%"}, 1, 20); err != nil || n != 0 {
		t.Fatalf("search wildcard escaped: %d %v", n, err)
	}
	coverage, err := Coverage(datasetID)
	if err != nil || len(coverage) != 2 || coverage[0].Records != 1 {
		t.Fatalf("coverage: %+v %v", coverage, err)
	}
	if _, n, err := ListFiltered(Filter{DatasetID: datasetID, SourceID: -1}, 1, 20); err != nil || n != 1 {
		t.Fatalf("unknown source filter: %d %v", n, err)
	}
	view, err := SaveView(datasetID, "Updated", filter)
	if err != nil {
		t.Fatal(err)
	}
	views, err := ListViews(datasetID)
	if err != nil || len(views) != 1 || views[0].ID != view.ID || views[0].Filter.Activity != "updated" {
		t.Fatalf("views: %+v %v", views, err)
	}
	if err := DeleteView(datasetID, view.ID); err != nil {
		t.Fatal(err)
	}
	data, count, err := BuildCSV(context.Background(), Filter{DatasetID: datasetID}, DirectExportLimit)
	if err != nil || count != 2 || !strings.Contains(string(data), "'=SUM(1,2)") {
		t.Fatalf("csv: count=%d err=%v content=%s", count, err, data)
	}
	if _, _, err := BuildCSV(context.Background(), Filter{DatasetID: datasetID}, 1); err == nil {
		t.Fatal("CSV row limit was not enforced")
	}
	job, err := CreateExportJob(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := ProcessNextExport(context.Background())
	if err != nil || !processed {
		t.Fatalf("process export: %v %v", processed, err)
	}
	finished, err := GetExportJob(job.ID)
	if err != nil || finished.Status != "ready" || finished.RowCount != 1 {
		t.Fatalf("job: %+v %v", finished, err)
	}
	health, err := dataset_repo.Health(project.Id)
	if err != nil || health.Created7d != 2 || health.Updated7d != 1 || health.LastEffectiveAt == nil || health.Issues != 1 || health.Collectors[0].Issue != "尚未发布" {
		t.Fatalf("health: %+v %v", health, err)
	}
}
