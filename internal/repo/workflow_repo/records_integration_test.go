package workflow_repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
	"github.com/nekoimi/scrapio/internal/workflow"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func TestDevRecordTemplatePublication(t *testing.T) {
	if os.Getenv("SCRAPIO_TEST_DEV_A04") != "1" && os.Getenv("SCRAPIO_TEST_DEV_C01") != "1" && os.Getenv("SCRAPIO_TEST_DEV_C02") != "1" {
		t.Skip("set SCRAPIO_TEST_DEV_C02=1 to test config/dev.yaml PostgreSQL")
	}
	previous := log.GetLevel()
	defer log.SetLevel(previous)
	log.SetLevel(log.ErrorLevel)
	v := viper.New()
	v.SetConfigFile("../../../config/dev.yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatal("cannot read dev config")
	}
	probe, err := sql.Open("postgres", v.GetString("db.dsn"))
	if err != nil {
		t.Fatal("cannot open development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var name string
	err = probe.QueryRowContext(ctx, "SELECT current_database()").Scan(&name)
	probe.Close()
	if err != nil || name != "get_magnet_dev" {
		t.Fatal("unexpected development database")
	}
	beanCtx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegisterPtr(beanCtx, &config.Config{DB: &config.DBConfig{Dsn: v.GetString("db.dsn")}})
	lifecycle := db.NewDBLifecycle()
	if err := lifecycle.Start(beanCtx); err != nil {
		t.Fatal("cannot initialize database")
	}
	defer lifecycle.Stop(beanCtx)
	db.Instance().ShowSQL(false)
	raw := db.Instance().DB().DB
	var projectID, datasetID, sourceID int64
	if err := raw.QueryRow("SELECT d.project_id,d.id FROM datasets d JOIN projects p ON p.id=d.project_id WHERE p.code='default' AND d.code='article'").Scan(&projectID, &datasetID); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("SELECT id FROM sources ORDER BY id LIMIT 1").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	for _, template := range workflow.Templates()[:2] {
		encoded, _ := json.Marshal(template.Definition)
		owner, version, err := Create(CreateWorkflowInput{ProjectID: &projectID, DatasetID: &datasetID, SourceID: &sourceID, Code: fmt.Sprintf("a04_publish_%d", time.Now().UnixNano()), Name: "A04 publication test", ResourceType: "article", Definition: string(encoded)})
		if err != nil {
			t.Fatal(err)
		}
		defer func(id int64) {
			if _, err := raw.Exec("DELETE FROM workflow_runs WHERE workflow_id=$1", id); err != nil {
				t.Error(err)
			}
			if _, err := raw.Exec("DELETE FROM workflows WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		}(owner.Id)
		if err := PublishVersion(version.Id); err == nil {
			t.Fatal("records draft published without a sample")
		}
		url := fmt.Sprintf("https://example.org/a05/%d", time.Now().UnixNano())
		contentType, content := "html", fmt.Sprintf(`<link rel="canonical" href="%s"><h1> First </h1><article>Body</article>`, url)
		if template.Code == "json_api" {
			contentType = "json"
			content = fmt.Sprintf(`{"items":[{"url":%q,"title":"First"},{"url":%q,"title":"Second"}]}`, url, url)
		}
		sample, err := SaveSample(SampleInput{VersionID: version.Id, Source: "paste", PageURL: url, ContentType: contentType, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		var before, after int
		var observationsBefore, observationsAfter, revisionsBefore, revisionsAfter, pluginBefore, pluginAfter int
		if err := raw.QueryRow("SELECT count(*) FROM records WHERE dataset_id=$1", datasetID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		_ = raw.QueryRow("SELECT count(*) FROM record_observations WHERE dataset_id=$1", datasetID).Scan(&observationsBefore)
		_ = raw.QueryRow("SELECT count(*) FROM record_revisions").Scan(&revisionsBefore)
		_ = raw.QueryRow("SELECT count(*) FROM plugin_tasks").Scan(&pluginBefore)
		preview, err := PreviewSample(version.Id, sample)
		if err != nil || !preview.Passed {
			t.Fatalf("sample dry-run: %#v %v", preview, err)
		}
		if template.Code == "json_api" && (len(preview.Decisions) != 2 || preview.Decisions[0].Decision != "created" || preview.Decisions[1].Decision != "updated") {
			t.Fatalf("batch decisions: %#v", preview.Decisions)
		}
		if err := raw.QueryRow("SELECT count(*) FROM records WHERE dataset_id=$1", datasetID).Scan(&after); err != nil || before != after {
			t.Fatalf("dry-run wrote records: %d %d %v", before, after, err)
		}
		_ = raw.QueryRow("SELECT count(*) FROM record_observations WHERE dataset_id=$1", datasetID).Scan(&observationsAfter)
		_ = raw.QueryRow("SELECT count(*) FROM record_revisions").Scan(&revisionsAfter)
		_ = raw.QueryRow("SELECT count(*) FROM plugin_tasks").Scan(&pluginAfter)
		if observationsBefore != observationsAfter || revisionsBefore != revisionsAfter || pluginBefore != pluginAfter {
			t.Fatal("dry-run wrote observations, revisions or plugin tasks")
		}
		if _, err := CheckSamples(version.Id); err != nil {
			t.Fatal(err)
		}
		if err := DeleteSample(sample.Id); err != nil {
			t.Fatal(err)
		}
		if err := PublishVersion(version.Id); err == nil {
			t.Fatal("deleted sample did not block publication")
		}
		sample, err = SaveSample(SampleInput{VersionID: version.Id, Source: "paste", PageURL: url, ContentType: contentType, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		invalidContent := "<h1>Missing key</h1>"
		if contentType == "json" {
			invalidContent = `{"items":[{"title":"Missing key"}]}`
		}
		invalidSample, err := SaveSample(SampleInput{VersionID: version.Id, Source: "paste", ContentType: contentType, Content: invalidContent})
		if err != nil {
			t.Fatal(err)
		}
		lastSample, err := SaveSample(SampleInput{VersionID: version.Id, Source: "paste", PageURL: url, ContentType: contentType, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		checks, checkErr := CheckSamples(version.Id)
		if checkErr == nil || len(checks) != 3 || !checks[0].Passed || checks[1].Passed || !checks[2].Passed || checks[2].SampleID != lastSample.Id {
			t.Fatalf("regression must report every sample after failure: %#v %v", checks, checkErr)
		}
		if err := PublishVersion(version.Id); err == nil {
			t.Fatal("published despite failed sample")
		}
		if err := DeleteSample(invalidSample.Id); err != nil {
			t.Fatal(err)
		}
		expectedFailure, err := SaveSample(SampleInput{VersionID: version.Id, Source: "paste", PageURL: url, ContentType: contentType, Content: invalidContent, ExpectedOutcome: SampleError, ExpectedError: "required field"})
		if err != nil {
			t.Fatal(err)
		}
		if checks, err := CheckSamples(version.Id); err != nil || len(checks) != 3 || !checks[2].Passed || checks[2].ActualOutcome != SampleError || checks[2].SampleID != expectedFailure.Id {
			t.Fatalf("expected failure regression: %#v %v", checks, err)
		}
		if err := PublishVersion(version.Id); err != nil {
			t.Fatal("non-magnet publication:", err)
		}
		if err := DeleteSample(sample.Id); err == nil {
			t.Fatal("published sample was deleted")
		}
		run, task, err := StartRun(owner.Id, "{}", nil)
		if err != nil || run == nil || task == nil {
			t.Fatalf("start published template: %v", err)
		}
		filtered, _, err := task_repo.ListRuns(task_repo.RunFilter{ProjectID: projectID, WorkflowID: owner.Id, Page: 1, Size: 20})
		if err != nil || len(filtered) != 1 || filtered[0].Id != run.Id {
			t.Fatalf("project workflow run filter: %#v %v", filtered, err)
		}
		otherProject, _, err := task_repo.ListRuns(task_repo.RunFilter{ProjectID: projectID + 1000000, WorkflowID: owner.Id, Page: 1, Size: 20})
		if err != nil || len(otherProject) != 0 {
			t.Fatalf("run leaked into another project: %#v %v", otherProject, err)
		}
		validDraft, err := CreateDraft(owner.Id, string(encoded), nil)
		if err != nil {
			t.Fatal(err)
		}
		var documentID int64
		if err := raw.QueryRow(`INSERT INTO documents(task_id,document_type,content,content_size,metadata) VALUES($1,$2,$3,$4,'{}'::jsonb) RETURNING id`, task.Id, contentType, content, len(content)).Scan(&documentID); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = raw.Exec("DELETE FROM documents WHERE id=$1", documentID) }()
		if _, err := SaveSample(SampleInput{VersionID: validDraft.Id, Source: "document", DocumentID: &documentID, ContentType: contentType, Content: "tampered"}); err == nil {
			t.Fatal("tampered historical document accepted")
		}
		history, err := SaveSample(SampleInput{VersionID: validDraft.Id, Source: "document", DocumentID: &documentID, ContentType: contentType, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec("DELETE FROM documents WHERE id=$1", documentID); err == nil {
			t.Fatal("historical document referenced by a sample was deleted")
		}
		if result, err := PreviewSample(validDraft.Id, history); err != nil || !result.Passed {
			t.Fatalf("historical preview: %#v %v", result, err)
		}
		if comparison, err := CompareVersionSamples(version.Id, validDraft.Id, version.Id); err != nil || len(comparison.Samples) != 3 || comparison.Changed != 0 {
			t.Fatalf("same definition comparison: %+v %v", comparison, err)
		}
		definition := template.Definition
		definition.Nodes = append(definition.Nodes, workflow.Node{Name: "remove_key", Type: "transform", Config: map[string]any{"operations": []any{map[string]any{"op": "delete", "field": "url"}}}})
		invalid, _ := json.Marshal(definition)
		draft, err := CreateDraft(owner.Id, string(invalid), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := PublishVersion(draft.Id); err == nil {
			t.Fatal("published template without dataset key")
		}
		if comparison, err := CompareVersionSamples(version.Id, draft.Id, version.Id); err != nil || comparison.Changed == 0 {
			t.Fatalf("changed definition comparison: %+v %v", comparison, err)
		}
		rollback, err := RollbackVersion(version.Id)
		if err != nil || rollback.Status != VersionDraft || rollback.Id == version.Id {
			t.Fatalf("rollback draft: %+v %v", rollback, err)
		}
		if copied, err := ListSamples(rollback.Id); err != nil || len(copied) != 3 {
			t.Fatalf("rollback samples: %+v %v", copied, err)
		}
		if checks, err := CheckSamples(rollback.Id); err != nil || len(checks) != 3 {
			t.Fatalf("rollback regression: %+v %v", checks, err)
		}
		if run.WorkflowVersionId != version.Id {
			t.Fatalf("historical run version changed: %+v", run)
		}
	}
	listing := workflow.Templates()[3].Definition
	listing.Trigger.URL = "https://example.org/list"
	listingEncoded, _ := json.Marshal(listing)
	listingOwner, listingVersion, err := Create(CreateWorkflowInput{ProjectID: &projectID, DatasetID: &datasetID, SourceID: &sourceID, Code: fmt.Sprintf("b01_publish_%d", time.Now().UnixNano()), Name: "B01 listing test", ResourceType: "article", Definition: string(listingEncoded)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = raw.Exec("DELETE FROM workflows WHERE id=$1", listingOwner.Id) }()
	listSample, err := SaveSample(SampleInput{VersionID: listingVersion.Id, Source: "paste", PageRole: "list", PageURL: listing.Trigger.URL, ContentType: "html", Content: `<a class="article-link" href="/detail/1">detail</a>`})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := PreviewSample(listingVersion.Id, listSample); err != nil || !result.Passed || len(result.Discovered) != 1 {
		t.Fatalf("list preview: %#v %v", result, err)
	}
	if err := PublishVersion(listingVersion.Id); err == nil {
		t.Fatal("listing published without detail sample")
	}
	detailSample, err := SaveSample(SampleInput{VersionID: listingVersion.Id, Source: "paste", PageRole: "detail", PageURL: "https://example.org/detail/1", ContentType: "html", Content: `<link rel="canonical" href="https://example.org/detail/1"><h1>Detail</h1>`})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := PreviewSample(listingVersion.Id, detailSample); err != nil || !result.Passed || len(result.Decisions) != 1 {
		t.Fatalf("detail preview: %#v %v", result, err)
	}
	emptySample, err := SaveSample(SampleInput{VersionID: listingVersion.Id, Source: "paste", PageRole: "list", PageURL: listing.Trigger.URL, ContentType: "html", Content: `<div>No items</div>`, ExpectedOutcome: SampleEmptyList})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := PreviewSample(listingVersion.Id, emptySample); err != nil || !result.Passed || result.ActualOutcome != SampleEmptyList {
		t.Fatalf("empty list preview: %#v %v", result, err)
	}
	if checks, err := CheckSamples(listingVersion.Id); err != nil || len(checks) != 3 || !checks[2].Passed {
		t.Fatalf("listing regression: %#v %v", checks, err)
	}
	if err := PublishVersion(listingVersion.Id); err != nil {
		t.Fatal("list and detail samples should allow publication:", err)
	}
	t.Log("article and JSON templates published; listing publication requires both list and detail samples")
}
