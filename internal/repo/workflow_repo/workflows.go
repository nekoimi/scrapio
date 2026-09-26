package workflow_repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/crawlpolicy"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
	"github.com/nekoimi/scrapio/internal/repo/resource_repo"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
	"github.com/nekoimi/scrapio/internal/workflow"
	"xorm.io/xorm"
)

const (
	VersionDraft     = "draft"
	VersionPublished = "published"
	VersionRetired   = "retired"
)

type WorkflowFilter struct {
	ProjectID *int64
	DatasetID *int64
	SourceID  *int64
	Enabled   *bool
	Page      int
	Size      int
}

type CreateWorkflowInput struct {
	ProjectID    *int64
	DatasetID    *int64
	SourceID     *int64
	Source       string
	SourceName   string
	Code         string
	Name         string
	ResourceType string
	Definition   string
	CreatedBy    *int64
}

func List(filter WorkflowFilter) ([]table.Workflow, int64, error) {
	if db.Instance() == nil {
		return nil, 0, errors.New("database is not initialized")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Size <= 0 {
		filter.Size = 20
	}
	count := db.Instance().NewSession()
	defer count.Close()
	applyFilter(count, filter)
	total, err := count.Count(new(table.Workflow))
	if err != nil {
		return nil, 0, err
	}
	s := db.Instance().NewSession()
	defer s.Close()
	applyFilter(s, filter)
	rows := make([]table.Workflow, 0)
	err = s.Desc("updated_at").Limit(filter.Size, (filter.Page-1)*filter.Size).Find(&rows)
	return rows, total, err
}

func applyFilter(s *xorm.Session, filter WorkflowFilter) {
	if filter.ProjectID != nil {
		s.And("project_id = ?", *filter.ProjectID)
	}
	if filter.DatasetID != nil {
		s.And("dataset_id = ?", *filter.DatasetID)
	}
	if filter.SourceID != nil {
		s.And("source_id = ?", *filter.SourceID)
	}
	if filter.Enabled != nil {
		s.And("enabled = ?", *filter.Enabled)
	}
}

// Get returns a workflow without mutating its versions.
func Get(id int64) (*table.Workflow, bool, error) {
	if id <= 0 || db.Instance() == nil {
		return nil, false, errors.New("workflow id is required")
	}
	row := new(table.Workflow)
	has, err := db.Instance().ID(id).Get(row)
	return row, has, err
}

func ListVersions(workflowID int64) ([]table.WorkflowVersion, error) {
	if workflowID <= 0 || db.Instance() == nil {
		return nil, errors.New("workflow id is required")
	}
	rows := make([]table.WorkflowVersion, 0)
	err := db.Instance().Where("workflow_id = ?", workflowID).Desc("version").Find(&rows)
	return rows, err
}

func GetVersion(id int64) (*table.WorkflowVersion, bool, error) {
	if id <= 0 || db.Instance() == nil {
		return nil, false, errors.New("workflow version id is required")
	}
	row := new(table.WorkflowVersion)
	has, err := db.Instance().ID(id).Get(row)
	return row, has, err
}

func Create(input CreateWorkflowInput) (*table.Workflow, *table.WorkflowVersion, error) {
	if db.Instance() == nil {
		return nil, nil, errors.New("database is not initialized")
	}
	input.Code, input.Name = strings.TrimSpace(input.Code), strings.TrimSpace(input.Name)
	if input.Code == "" || input.Name == "" {
		return nil, nil, errors.New("workflow code and name are required")
	}
	if input.ResourceType == "" {
		input.ResourceType = "magnet"
	}
	if strings.TrimSpace(input.Definition) == "" {
		return nil, nil, errors.New("workflow definition is required")
	}
	if _, err := workflow.ParseDefinition(input.Definition); err != nil {
		return nil, nil, err
	}
	projectID := input.ProjectID
	if projectID == nil {
		defaultProject := new(table.Project)
		if has, err := db.Instance().Where("code = ?", "default").Get(defaultProject); err != nil || !has {
			if err != nil {
				return nil, nil, err
			}
			return nil, nil, errors.New("default project not found")
		}
		projectID = &defaultProject.Id
	}
	project := new(table.Project)
	if has, err := db.Instance().ID(*projectID).Get(project); err != nil || !has {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("project not found")
	}
	datasetID := input.DatasetID
	if datasetID == nil && input.ResourceType == "magnet" {
		defaultDataset := new(table.Dataset)
		if has, err := db.Instance().Where("project_id = ? AND code = ?", *projectID, "magnet").Get(defaultDataset); err != nil {
			return nil, nil, err
		} else if has {
			datasetID = &defaultDataset.Id
		}
	}
	if datasetID != nil {
		dataset := new(table.Dataset)
		if has, err := db.Instance().ID(*datasetID).Get(dataset); err != nil || !has {
			if err != nil {
				return nil, nil, err
			}
			return nil, nil, errors.New("dataset not found")
		}
		if dataset.ProjectId != *projectID || dataset.RecordType != input.ResourceType {
			return nil, nil, errors.New("dataset must belong to project and match resource type")
		}
	}
	if datasetID == nil {
		return nil, nil, errors.New("dataset is required for new workflows")
	}
	sourceID, err := resource_repo.ResolveSourceID(input.SourceID, input.Source, input.SourceName)
	if err != nil {
		return nil, nil, err
	}
	row := &table.Workflow{ProjectId: projectID, DatasetId: datasetID, SourceId: sourceID, Code: input.Code, Name: input.Name, ResourceType: input.ResourceType, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	version := &table.WorkflowVersion{Version: 1, Status: VersionDraft, Definition: input.Definition, CreatedBy: input.CreatedBy, CreatedAt: time.Now()}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, nil, err
	}
	if _, err := s.Insert(row); err != nil {
		_ = s.Rollback()
		return nil, nil, err
	}
	version.WorkflowId = row.Id
	if _, err := s.Insert(version); err != nil {
		_ = s.Rollback()
		return nil, nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, nil, err
	}
	return row, version, nil
}

func CreateDraft(workflowID int64, definition string, createdBy *int64) (*table.WorkflowVersion, error) {
	if _, has, err := Get(workflowID); err != nil || !has {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("workflow not found")
	}
	if _, err := workflow.ParseDefinition(definition); err != nil {
		return nil, err
	}
	var latest struct {
		Version int `xorm:"version"`
	}
	if _, err := db.Instance().Table(new(table.WorkflowVersion)).Select("COALESCE(MAX(version), 0) AS version").Where("workflow_id = ?", workflowID).Get(&latest); err != nil {
		return nil, err
	}
	version := &table.WorkflowVersion{WorkflowId: workflowID, Version: latest.Version + 1, Status: VersionDraft, Definition: definition, CreatedBy: createdBy, CreatedAt: time.Now()}
	_, err := db.Instance().InsertOne(version)
	return version, err
}

func ValidateVersion(id int64) error {
	version, has, err := GetVersion(id)
	if err != nil {
		return err
	}
	if !has {
		return errors.New("workflow version not found")
	}
	owner, has, err := Get(version.WorkflowId)
	if err != nil {
		return err
	}
	if !has {
		return errors.New("workflow not found")
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil {
		return &workflow.DefinitionError{Cause: err}
	}
	if definition.Persistence == "records" {
		if owner.DatasetId == nil {
			return &workflow.DefinitionError{Cause: errors.New("dataset_id: required")}
		}
		schema, err := record_repo.DatasetSchema(*owner.DatasetId)
		if err != nil {
			return &workflow.DefinitionError{Cause: fmt.Errorf("dataset: %w", err)}
		}
		role := "trigger"
		if definition.Listing != nil {
			role = "detail"
		}
		if err := definition.ValidateRecordSchema(schema, role); err != nil {
			return &workflow.DefinitionError{Cause: err}
		}
		return nil
	}
	if owner.ResourceType != "magnet" {
		return &workflow.DefinitionError{Cause: fmt.Errorf("resource_type: %q cannot be persisted by the current worker", owner.ResourceType)}
	}
	return nil
}

func PublishVersion(id int64) error {
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	locked, err := s.QueryString("SELECT id,workflow_id,status FROM workflow_versions WHERE id=? FOR UPDATE", id)
	if err != nil {
		return err
	}
	if len(locked) == 0 {
		return errors.New("workflow version not found")
	}
	if locked[0]["status"] != VersionDraft && locked[0]["status"] != VersionRetired {
		return errors.New("workflow version is already published")
	}
	version, has, err := GetVersion(id)
	if err != nil || !has {
		return errors.New("workflow version not found")
	}
	owner, has, err := Get(version.WorkflowId)
	if err != nil || !has {
		return errors.New("workflow not found")
	}
	if owner.DatasetId != nil {
		rows, err := s.QueryString("SELECT id FROM datasets WHERE id=? FOR SHARE", *owner.DatasetId)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errors.New("dataset not found")
		}
	}
	if err := ValidateVersion(id); err != nil {
		return err
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil {
		return &workflow.DefinitionError{Cause: err}
	}
	// Existing retired versions predate A05. Fresh records drafts must pass
	// every immutable sample while the version row excludes sample creation.
	if definition.Persistence == "records" && locked[0]["status"] == VersionDraft {
		if _, err := CheckSamples(id); err != nil {
			return err
		}
	}
	now := time.Now()
	if _, err := s.Where("workflow_id = ? AND status = ?", version.WorkflowId, VersionPublished).Cols("status").Update(&table.WorkflowVersion{Status: VersionRetired}); err != nil {
		return err
	}
	if _, err := s.ID(id).Cols("status", "published_at").Update(&table.WorkflowVersion{Status: VersionPublished, PublishedAt: &now}); err != nil {
		return err
	}
	if _, err := s.ID(version.WorkflowId).Cols("published_version_id", "updated_at", "enabled").Update(&table.Workflow{PublishedVersionId: &id, UpdatedAt: now, Enabled: true}); err != nil {
		return err
	}
	return s.Commit()
}

// RollbackVersion copies historical rules and samples into a new draft.
// Publication remains a separate, validated action.
func RollbackVersion(id int64) (*table.WorkflowVersion, error) {
	source, has, err := GetVersion(id)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.New("workflow version not found")
	}
	if source.Status == VersionDraft {
		return nil, errors.New("rollback source must be a published or retired version")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT id FROM workflows WHERE id=? FOR UPDATE", source.WorkflowId)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("workflow not found")
	}
	var latest struct {
		Version int `xorm:"version"`
	}
	if _, err := s.Table(new(table.WorkflowVersion)).Select("COALESCE(MAX(version),0) AS version").Where("workflow_id=?", source.WorkflowId).Get(&latest); err != nil {
		return nil, err
	}
	draft := &table.WorkflowVersion{WorkflowId: source.WorkflowId, Version: latest.Version + 1, Status: VersionDraft, Definition: source.Definition, CreatedAt: time.Now()}
	if _, err := s.Insert(draft); err != nil {
		return nil, err
	}
	_, err = s.Exec(`INSERT INTO workflow_samples(workflow_version_id,source,document_id,page_url,content_type,content,content_hash,note,page_role,expected_outcome,expected_error)
SELECT ?,source,document_id,page_url,content_type,content,content_hash,note,page_role,expected_outcome,expected_error
FROM workflow_samples WHERE workflow_version_id=? ORDER BY id`, draft.Id, id)
	if err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return draft, nil
}

func Stop(id int64) error {
	if _, has, err := Get(id); err != nil || !has {
		if err != nil {
			return err
		}
		return errors.New("workflow not found")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	if _, err := s.QueryString("SELECT workflow_id FROM workflow_schedules WHERE workflow_id=? FOR UPDATE", id); err != nil {
		_ = s.Rollback()
		return err
	}
	if _, err := s.ID(id).Cols("enabled", "updated_at").Update(&table.Workflow{Enabled: false, UpdatedAt: time.Now()}); err != nil {
		_ = s.Rollback()
		return err
	}
	if _, err := s.Exec("UPDATE workflow_schedules SET enabled=false,next_run_at=NULL,updated_at=NOW() WHERE workflow_id=?", id); err != nil {
		_ = s.Rollback()
		return err
	}
	if _, err := s.Exec("UPDATE workflow_schedule_events SET status='skipped',reason='workflow stopped' WHERE workflow_id=? AND status='pending'", id); err != nil {
		_ = s.Rollback()
		return err
	}
	return s.Commit()
}

// StartRun creates a durable run and its root task. Execution is deliberately
// decoupled from this API so a later worker can claim the same task record.
func StartRun(workflowID int64, input string, createdBy *int64) (*table.WorkflowRun, *table.CrawlTask, error) {
	return StartRunWithTrigger(workflowID, input, createdBy, "manual")
}

func StartRunWithTrigger(workflowID int64, input string, createdBy *int64, trigger string) (*table.WorkflowRun, *table.CrawlTask, error) {
	if trigger != "manual" && trigger != "api" && trigger != "cron" {
		return nil, nil, errors.New("unsupported trigger type")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, nil, err
	}
	run, task, err := startRunTx(s, workflowID, input, createdBy, trigger)
	if err != nil {
		_ = s.Rollback()
		return nil, nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, nil, err
	}
	return run, task, nil
}

// startRunTx is shared with the durable scheduler so an event and its run commit together.
func startRunTx(s *xorm.Session, workflowID int64, input string, createdBy *int64, trigger string) (*table.WorkflowRun, *table.CrawlTask, error) {
	// Serialize run creation and the scheduler's concurrency decision per workflow.
	if _, err := s.QueryString("SELECT id FROM workflows WHERE id=? FOR UPDATE", workflowID); err != nil {
		return nil, nil, err
	}
	row := new(table.Workflow)
	has, err := s.ID(workflowID).Get(row)
	if err != nil || !has {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("workflow not found")
	}
	if !row.Enabled || row.PublishedVersionId == nil {
		return nil, nil, errors.New("workflow has no published version")
	}
	if err := ValidateVersion(*row.PublishedVersionId); err != nil {
		return nil, nil, fmt.Errorf("published workflow cannot execute: %w", err)
	}
	if input == "" {
		input = "{}"
	}
	// Run input is an arbitrary JSON object, unlike the workflow definition.
	var value map[string]any
	if jsonErr := json.Unmarshal([]byte(input), &value); jsonErr != nil {
		return nil, nil, fmt.Errorf("invalid run input: %w", jsonErr)
	}
	if value == nil {
		return nil, nil, errors.New("run input must be a JSON object")
	}
	if _, hasURL := value["url"]; hasURL {
		return nil, nil, errors.New("run input cannot override the published entry URL")
	}
	now := time.Now()
	run := &table.WorkflowRun{WorkflowId: workflowID, WorkflowVersionId: *row.PublishedVersionId, TriggerType: trigger, Status: task_repo.RunQueued, Input: input, Summary: "{}", CreatedBy: createdBy, CreatedAt: now}
	task := &table.CrawlTask{StepName: "trigger", TaskType: "workflow", Input: input, Status: task_repo.TaskQueued, MaxAttempts: 5, CreatedAt: now, UpdatedAt: now}
	version, has, err := GetVersion(*row.PublishedVersionId)
	if err != nil || !has {
		return nil, nil, errors.New("published version not found")
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil {
		return nil, nil, err
	}
	if definition.Listing != nil {
		// Root metadata comes from the definition, never caller-supplied input.
		value["page_role"], value["url"], value["page_index"] = "list", definition.Trigger.URL, 1
		delete(value, "empty_streak")
		encoded, _ := json.Marshal(value)
		task.Input = string(encoded)
	}
	if _, err := s.Insert(run); err != nil {
		return nil, nil, err
	}
	budget := crawlpolicy.Defaults(definition.Trigger.URL)
	if definition.Budget != nil {
		budget = *definition.Budget
	}
	budgetJSON, _ := json.Marshal(budget)
	if _, err := s.Exec("UPDATE workflow_runs SET budget=CAST(? AS jsonb) WHERE id=?", string(budgetJSON), run.Id); err != nil {
		return nil, nil, err
	}
	run.Budget = string(budgetJSON)
	task.RunId = run.Id
	if _, err := s.Insert(task); err != nil {
		return nil, nil, err
	}
	return run, task, nil
}

func DiffVersions(leftID, rightID int64) (map[string]any, error) {
	left, has, err := GetVersion(leftID)
	if err != nil || !has {
		return nil, fmt.Errorf("left workflow version not found")
	}
	right, has, err := GetVersion(rightID)
	if err != nil || !has {
		return nil, fmt.Errorf("right workflow version not found")
	}
	var leftValue, rightValue any
	if err := json.Unmarshal([]byte(left.Definition), &leftValue); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(right.Definition), &rightValue); err != nil {
		return nil, err
	}
	return map[string]any{"left_version_id": leftID, "right_version_id": rightID, "changed": !jsonEqual(leftValue, rightValue), "left": leftValue, "right": rightValue}, nil
}

func jsonEqual(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
