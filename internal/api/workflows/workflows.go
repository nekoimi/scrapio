package workflows

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/error_ext"
	"github.com/nekoimi/scrapio/internal/pkg/request"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/audit_repo"
	"github.com/nekoimi/scrapio/internal/repo/workflow_repo"
	"github.com/nekoimi/scrapio/internal/workflow"
)

type ListRequest struct {
	ProjectID *int64 `json:"project_id,omitempty"`
	DatasetID *int64 `json:"dataset_id,omitempty"`
	Page      int    `json:"page,omitempty"`
	Size      int    `json:"size,omitempty"`
	SourceID  *int64 `json:"source_id,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

type CreateRequest struct {
	ProjectID    *int64 `json:"project_id,omitempty"`
	DatasetID    *int64 `json:"dataset_id,omitempty"`
	SourceID     *int64 `json:"source_id,omitempty"`
	Source       string `json:"source,omitempty"`
	SourceName   string `json:"source_name,omitempty"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	ResourceType string `json:"resource_type,omitempty"`
	Definition   string `json:"definition"`
}

type VersionRequest struct {
	WorkflowID    int64  `json:"workflow_id"`
	Definition    string `json:"definition"`
	BaseVersionID int64  `json:"base_version_id,omitempty"`
	ChangeSummary string `json:"change_summary,omitempty"`
}
type OwnerRequest struct {
	WorkflowID int64  `json:"workflow_id"`
	OwnerName  string `json:"owner_name"`
}
type IDRequest struct {
	ID int64 `json:"id"`
}
type DiffRequest struct {
	LeftID  int64 `json:"left_id"`
	RightID int64 `json:"right_id"`
}
type ExtractRequest struct {
	ContentType string               `json:"content_type"`
	Content     string               `json:"content"`
	Fields      []workflow.FieldRule `json:"fields"`
}
type ReplayRequest struct {
	DocumentID int64 `json:"document_id"`
	VersionID  int64 `json:"version_id"`
}
type ReplayDiffRequest struct {
	DocumentID   int64 `json:"document_id"`
	LeftVersion  int64 `json:"left_version_id"`
	RightVersion int64 `json:"right_version_id"`
}
type CompareSamplesRequest struct {
	LeftVersionID   int64 `json:"left_version_id"`
	RightVersionID  int64 `json:"right_version_id"`
	SampleVersionID int64 `json:"sample_version_id"`
}
type RunRequest struct {
	WorkflowID int64           `json:"workflow_id"`
	Input      json.RawMessage `json:"input,omitempty"`
}

func Templates(w http.ResponseWriter, r *http.Request) {
	respond.Ok(w, map[string]any{"list": workflow.Templates()})
}

func List(w http.ResponseWriter, r *http.Request) {
	input := ListRequest{}
	if r.Method == http.MethodPost {
		if err := request.Parse(r, &input); err != nil {
			respond.Error(w, err)
			return
		}
	}
	query := r.URL.Query()
	if input.Page == 0 {
		input.Page, _ = strconv.Atoi(query.Get("page"))
	}
	if input.Size == 0 {
		input.Size, _ = strconv.Atoi(query.Get("size"))
	}
	if input.SourceID == nil && query.Get("source_id") != "" {
		value, _ := strconv.ParseInt(query.Get("source_id"), 10, 64)
		input.SourceID = &value
	}
	if input.ProjectID == nil && query.Get("project_id") != "" {
		value, _ := strconv.ParseInt(query.Get("project_id"), 10, 64)
		input.ProjectID = &value
	}
	if input.DatasetID == nil && query.Get("dataset_id") != "" {
		value, _ := strconv.ParseInt(query.Get("dataset_id"), 10, 64)
		input.DatasetID = &value
	}
	rows, total, err := workflow_repo.List(workflow_repo.WorkflowFilter{Page: input.Page, Size: input.Size, SourceID: input.SourceID, ProjectID: input.ProjectID, DatasetID: input.DatasetID, Enabled: input.Enabled})
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"list": rows, "total": total})
}

func Detail(w http.ResponseWriter, r *http.Request) {
	id, err := idFromRequest(r)
	if err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	row, has, err := workflow_repo.Get(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	versions, err := workflow_repo.ListVersions(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"workflow": row, "versions": versions})
}

func Create(w http.ResponseWriter, r *http.Request) {
	input := new(CreateRequest)
	if err := request.Parse(r, input); err != nil {
		respond.Error(w, err)
		return
	}
	row, version, err := workflow_repo.Create(workflow_repo.CreateWorkflowInput{ProjectID: input.ProjectID, DatasetID: input.DatasetID, SourceID: input.SourceID, Source: input.Source, SourceName: input.SourceName, Code: input.Code, Name: input.Name, ResourceType: input.ResourceType, Definition: input.Definition})
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"workflow": row, "version": version})
}

func CreateVersion(w http.ResponseWriter, r *http.Request) {
	input := new(VersionRequest)
	if err := request.Parse(r, input); err != nil || input.WorkflowID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	version, err := workflow_repo.CreateDraftWithBase(input.WorkflowID, input.Definition, nil, input.BaseVersionID, input.ChangeSummary)
	if err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.version_created", "workflow", &input.WorkflowID, map[string]any{"version_id": version.Id})
	respond.Ok(w, version)
}

func UpdateOwner(w http.ResponseWriter, r *http.Request) {
	input := new(OwnerRequest)
	if err := request.Parse(r, input); err != nil || input.WorkflowID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := workflow_repo.UpdateOwner(input.WorkflowID, input.OwnerName); err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.owner_updated", "workflow", &input.WorkflowID, map[string]any{"owner_name": input.OwnerName})
	respond.Ok(w, nil)
}

func Run(w http.ResponseWriter, r *http.Request) {
	input := new(RunRequest)
	if err := request.Parse(r, input); err != nil || input.WorkflowID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	runInput := string(input.Input)
	if runInput == "" {
		runInput = "{}"
	}
	run, task, err := workflow_repo.StartRun(input.WorkflowID, runInput, nil)
	if err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.run_created", "workflow_run", &run.Id, map[string]any{"workflow_id": input.WorkflowID, "task_id": task.Id})
	respond.Ok(w, map[string]any{"run_id": run.Id, "task_id": task.Id, "workflow_version_id": run.WorkflowVersionId})
}

func Validate(w http.ResponseWriter, r *http.Request) {
	mutateVersion(w, r, workflow_repo.ValidateVersion, true)
}
func Publish(w http.ResponseWriter, r *http.Request) {
	id, err := idFromRequest(r)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := workflow_repo.PublishVersion(id); err != nil {
		if workflow.IsDefinitionError(err) {
			path, reason := workflow.Issue(err)
			respond.InvalidDefinition(w, path, reason)
			return
		}
		respond.Error(w, err)
		return
	}
	version, has, err := workflow_repo.GetVersion(id)
	if err == nil && has {
		_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.version_published", "workflow", &version.WorkflowId, map[string]any{"version_id": id})
	}
	respond.Ok(w, nil)
}
func Rollback(w http.ResponseWriter, r *http.Request) {
	id, err := idFromRequest(r)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	draft, err := workflow_repo.RollbackVersion(id)
	if err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.rollback_draft_created", "workflow", &draft.WorkflowId, map[string]any{"source_version_id": id, "draft_version_id": draft.Id})
	respond.Ok(w, draft)
}

func CompareSamples(w http.ResponseWriter, r *http.Request) {
	input := new(CompareSamplesRequest)
	if err := request.Parse(r, input); err != nil || input.LeftVersionID <= 0 || input.RightVersionID <= 0 || input.SampleVersionID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	result, err := workflow_repo.CompareVersionSamples(input.LeftVersionID, input.RightVersionID, input.SampleVersionID)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}

func Stop(w http.ResponseWriter, r *http.Request) {
	id, err := idFromRequest(r)
	if err != nil {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := workflow_repo.Stop(id); err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, nil)
}

func Diff(w http.ResponseWriter, r *http.Request) {
	input := new(DiffRequest)
	if err := request.Parse(r, input); err != nil || input.LeftID <= 0 || input.RightID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	result, err := workflow_repo.DiffVersions(input.LeftID, input.RightID)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}

func TestExtract(w http.ResponseWriter, r *http.Request) {
	input := new(ExtractRequest)
	if err := request.Parse(r, input); err != nil {
		respond.Error(w, err)
		return
	}
	result, err := workflow.Extract(workflow.ExtractRequest{ContentType: input.ContentType, Content: input.Content, Fields: input.Fields})
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, result)
}

func Replay(w http.ResponseWriter, r *http.Request) {
	input := new(ReplayRequest)
	if err := request.Parse(r, input); err != nil || input.DocumentID <= 0 || input.VersionID <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	document := new(table.Document)
	has, err := db.Instance().ID(input.DocumentID).Get(document)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	version, has, err := workflow_repo.GetVersion(input.VersionID)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	values, err := replayDocument(document, version)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"document_id": input.DocumentID, "version_id": input.VersionID, "values": values})
}

func ReplayDiff(w http.ResponseWriter, r *http.Request) {
	input := new(ReplayDiffRequest)
	if err := request.Parse(r, input); err != nil || input.DocumentID <= 0 || input.LeftVersion <= 0 || input.RightVersion <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	document := new(table.Document)
	has, err := db.Instance().ID(input.DocumentID).Get(document)
	if err != nil {
		respond.Error(w, err)
		return
	}
	if !has {
		respond.Error(w, error_ext.DataNotFoundError)
		return
	}
	left, has, err := workflow_repo.GetVersion(input.LeftVersion)
	if err != nil || !has {
		if err == nil {
			err = error_ext.DataNotFoundError
		}
		respond.Error(w, err)
		return
	}
	right, has, err := workflow_repo.GetVersion(input.RightVersion)
	if err != nil || !has {
		if err == nil {
			err = error_ext.DataNotFoundError
		}
		respond.Error(w, err)
		return
	}
	leftValues, err := replayDocument(document, left)
	if err != nil {
		respond.Error(w, err)
		return
	}
	rightValues, err := replayDocument(document, right)
	if err != nil {
		respond.Error(w, err)
		return
	}
	leftJSON, _ := json.Marshal(leftValues)
	rightJSON, _ := json.Marshal(rightValues)
	respond.Ok(w, map[string]any{"document_id": input.DocumentID, "left_version_id": input.LeftVersion, "right_version_id": input.RightVersion, "changed": string(leftJSON) != string(rightJSON), "left": leftValues, "right": rightValues})
}

func replayDocument(document *table.Document, version *table.WorkflowVersion) (map[string]any, error) {
	definition, err := workflow.ParseDefinition(version.Definition)
	if err != nil {
		return nil, err
	}
	fields, err := extractionFields(definition)
	if err != nil {
		return nil, err
	}
	return workflow.Extract(workflow.ExtractRequest{ContentType: document.DocumentType, Content: document.Content, Fields: fields})
}

func extractionFields(definition workflow.Definition) ([]workflow.FieldRule, error) {
	for _, node := range append(append([]workflow.Node{}, definition.Acquire...), definition.Nodes...) {
		if !strings.EqualFold(node.Type, "extract") {
			continue
		}
		raw, ok := node.Config["fields"]
		if !ok {
			return nil, errors.New("extract.fields is missing")
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var fields []workflow.FieldRule
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return nil, err
		}
		return fields, nil
	}
	return nil, errors.New("workflow has no extract node")
}

func mutateVersion(w http.ResponseWriter, r *http.Request, action func(int64) error, validation bool) {
	id, err := idFromRequest(r)
	if err != nil || id <= 0 {
		respond.Error(w, error_ext.ValidateError)
		return
	}
	if err := action(id); err != nil {
		if validation && workflow.IsDefinitionError(err) {
			path, reason := workflow.Issue(err)
			respond.InvalidDefinition(w, path, reason)
			return
		}
		respond.Error(w, err)
		return
	}
	respond.Ok(w, nil)
}

func idFromRequest(r *http.Request) (int64, error) {
	if raw := r.URL.Query().Get("id"); raw != "" {
		return strconv.ParseInt(raw, 10, 64)
	}
	var input IDRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return 0, err
	}
	return input.ID, nil
}
