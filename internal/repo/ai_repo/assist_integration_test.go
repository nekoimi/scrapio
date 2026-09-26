package ai_repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/repo/dataset_repo"
	"github.com/nekoimi/scrapio/internal/repo/workflow_repo"
	"github.com/spf13/viper"
)

func TestDevD03SuggestionReview(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_D03") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_D03=1 to test dev PostgreSQL")
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
	project, err := dataset_repo.CreateProject(fmt.Sprintf("d03_%d", time.Now().UnixNano()), "D03 fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := dataset_repo.CreateDataset(dataset_repo.CreateDatasetInput{ProjectID: project.Id, Code: "items", Name: "D03 records", RecordType: "article", SchemaInput: dataset_repo.SchemaInput{UniqueKeyFields: []string{"url"}, EmptyValuePolicy: "preserve", Fields: []dataset_repo.FieldInput{{Key: "url", Label: "URL", Type: "url", Required: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	var workflowID, versionID, sampleID, sourceID int64
	defer func() {
		for _, item := range []struct {
			query string
			id    int64
		}{{"DELETE FROM ai_rule_suggestions WHERE version_id=$1", versionID}, {"DELETE FROM ai_assist_requests WHERE version_id=$1", versionID}, {"DELETE FROM workflow_samples WHERE id=$1", sampleID}, {"DELETE FROM workflow_versions WHERE id=$1", versionID}, {"DELETE FROM workflows WHERE id=$1", workflowID}, {"DELETE FROM datasets WHERE id=$1", dataset.Id}, {"DELETE FROM projects WHERE id=$1", project.Id}} {
			if _, err := probe.Exec(item.query, item.id); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := probe.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflows(source_id,project_id,dataset_id,code,name,resource_type) VALUES($1,$2,$3,$4,'D03','article') RETURNING id", sourceID, project.Id, dataset.Id, fmt.Sprintf("d03_%d", time.Now().UnixNano())).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_versions(workflow_id,version,status,definition) VALUES($1,1,'draft','{}') RETURNING id", workflowID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow("INSERT INTO workflow_samples(workflow_version_id,source,page_role,content_type,content,content_hash) VALUES($1,'paste','trigger','html','<h1>test</h1>',$2) RETURNING id", versionID, fmt.Sprintf("%064d", sampleID)).Scan(&sampleID); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	var count int
	if err := probe.QueryRow("SELECT count(*) FROM ai_assist_requests WHERE created_at >= date_trunc('day',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	requestID, err := Reserve(workflowID, versionID, sampleID, count+1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reserve(workflowID, versionID, sampleID, count+1); err == nil {
		t.Fatal("daily cap was ignored")
	}
	row, err := SaveRuleSuggestion(requestID, workflowID, versionID, sampleID, "fixture", "fixture-hash", ai.AssistResponse{Model: "fixture", Suggestions: []ai.RuleSuggestion{{Field: "url", Selector: "a", Reason: "link"}}, Explanation: "fixed", InputTokens: 10, OutputTokens: 20})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending_review" || row.CreatedAt.Before(before.Add(-time.Minute)) {
		t.Fatalf("new suggestion status: %+v", row)
	}
	var decoded ai.AssistResponse
	if err := json.Unmarshal([]byte(row.Result), &decoded); err != nil || decoded.Suggestions[0].Field != "url" {
		t.Fatalf("suggestion result was not readable: %v", err)
	}
	rows, err := ListRuleSuggestions(versionID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %+v %v", rows, err)
	}
	reviewed, err := ReviewRuleSuggestion(row.ID, versionID, "accepted")
	if err != nil || reviewed.Status != "accepted" {
		t.Fatalf("review: %+v %v", reviewed, err)
	}
	if _, err := ReviewRuleSuggestion(row.ID, versionID, "rejected"); err == nil {
		t.Fatal("second review accepted")
	}
	if err := workflow_repo.DeleteSample(sampleID); err != nil {
		t.Fatalf("delete draft sample after AI suggestion: %v", err)
	}
	var remaining int
	if err := probe.QueryRow("SELECT count(*) FROM ai_rule_suggestions WHERE sample_id=$1", sampleID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("suggestions remain after sample deletion: %d %v", remaining, err)
	}
	if err := probe.QueryRow("SELECT count(*) FROM ai_assist_requests WHERE sample_id=$1", sampleID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("request reservations remain after sample deletion: %d %v", remaining, err)
	}
}
