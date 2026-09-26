package workflows

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nekoimi/scrapio/internal/ai"
	"github.com/nekoimi/scrapio/internal/api/middleware"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/pkg/respond"
	"github.com/nekoimi/scrapio/internal/repo/ai_repo"
	"github.com/nekoimi/scrapio/internal/repo/audit_repo"
	"github.com/nekoimi/scrapio/internal/repo/workflow_repo"
	"github.com/nekoimi/scrapio/internal/workflow"
)

type assistSelection struct {
	VersionID int64  `json:"version_id"`
	SampleID  int64  `json:"sample_id"`
	Failure   string `json:"failure"`
}

func assistContext(versionID, sampleID int64) (*table.WorkflowVersion, *table.WorkflowSample, []ai.RuleField, error) {
	version, found, err := workflow_repo.GetVersion(versionID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !found || version.Status != workflow_repo.VersionDraft {
		return nil, nil, nil, errors.New("select a draft version for AI assistance")
	}
	sample, err := workflow_repo.GetSample(sampleID)
	if err != nil {
		return nil, nil, nil, err
	}
	if sample.WorkflowVersionId != versionID {
		return nil, nil, nil, errors.New("sample does not belong to draft version")
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil || definition.Persistence != "records" {
		return nil, nil, nil, errors.New("AI assistance requires an executable records draft")
	}
	var fields []ai.RuleField
	raw, err := json.Marshal(definition.Nodes[0].Config["fields"])
	if err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, nil, err
	}
	if len(fields) == 0 || len(fields) > 100 {
		return nil, nil, nil, errors.New("draft requires 1–100 mapped fields")
	}
	return version, sample, fields, nil
}

func AIPrepare(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionID, _ := strconv.ParseInt(r.URL.Query().Get("version_id"), 10, 64)
		sampleID, _ := strconv.ParseInt(r.URL.Query().Get("sample_id"), 10, 64)
		if cfg == nil || cfg.AIAssist == nil || !cfg.AIAssist.Enabled {
			respond.Error(w, errors.New("AI assistance is disabled"))
			return
		}
		_, sample, fields, err := assistContext(versionID, sampleID)
		if err != nil {
			respond.Error(w, err)
			return
		}
		content, truncated := ai.TrimDocument(sample.ContentType, sample.Content)
		respond.Ok(w, map[string]any{"sample_id": sample.Id, "content_hash": sample.ContentHash, "content_type": sample.ContentType, "content": content, "truncated": truncated, "fields": fields, "model": cfg.AIAssist.Model, "max_requests_per_day": cfg.AIAssist.MaxRequestsPerDay})
	}
}

func AISuggest(cfg *config.Config, client ai.AssistClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var selection assistSelection
		if err := json.NewDecoder(r.Body).Decode(&selection); err != nil || selection.VersionID <= 0 || selection.SampleID <= 0 || len(selection.Failure) > 1000 {
			respond.Error(w, errors.New("invalid AI suggestion request"))
			return
		}
		if cfg == nil || cfg.AIAssist == nil || !cfg.AIAssist.Enabled {
			respond.Error(w, errors.New("AI assistance is disabled"))
			return
		}
		version, sample, fields, err := assistContext(selection.VersionID, selection.SampleID)
		if err != nil {
			respond.Error(w, err)
			return
		}
		content, _ := ai.TrimDocument(sample.ContentType, sample.Content)
		input := ai.AssistRequest{ContentType: sample.ContentType, Content: content, PageRole: sample.PageRole, Failure: strings.TrimSpace(selection.Failure), Fields: fields}
		requestID, err := ai_repo.Reserve(version.WorkflowId, version.Id, sample.Id, cfg.AIAssist.MaxRequestsPerDay)
		if err != nil {
			respond.Error(w, err)
			return
		}
		result, err := client.Suggest(r.Context(), cfg.AIAssist, input)
		if err == nil {
			err = ai.ValidateSuggestions(input, result)
		}
		if err != nil {
			ai_repo.FailRequest(requestID)
			respond.Error(w, err)
			return
		}
		row, err := ai_repo.SaveRuleSuggestion(requestID, version.WorkflowId, version.Id, sample.Id, result.Model, sample.ContentHash, result)
		if err != nil {
			ai_repo.FailRequest(requestID)
			respond.Error(w, err)
			return
		}
		_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.ai_suggestion_created", "workflow", &version.WorkflowId, map[string]any{"suggestion_id": row.ID, "version_id": version.Id, "sample_id": sample.Id, "model": result.Model, "input_tokens": result.InputTokens, "output_tokens": result.OutputTokens})
		respond.Ok(w, row)
	}
}

func AIList(w http.ResponseWriter, r *http.Request) {
	versionID, _ := strconv.ParseInt(r.URL.Query().Get("version_id"), 10, 64)
	rows, err := ai_repo.ListRuleSuggestions(versionID)
	if err != nil {
		respond.Error(w, err)
		return
	}
	respond.Ok(w, map[string]any{"list": rows})
}

func AIReview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID        int64  `json:"id"`
		VersionID int64  `json:"version_id"`
		Decision  string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.ID <= 0 || input.VersionID <= 0 {
		respond.Error(w, errors.New("invalid review request"))
		return
	}
	version, found, err := workflow_repo.GetVersion(input.VersionID)
	if err != nil || !found || version.Status != workflow_repo.VersionDraft {
		respond.Error(w, errors.New("only draft suggestions can be reviewed"))
		return
	}
	row, err := ai_repo.ReviewRuleSuggestion(input.ID, input.VersionID, input.Decision)
	if err != nil {
		respond.Error(w, err)
		return
	}
	_ = audit_repo.Record(middleware.RequestID(r.Context()), "workflow.ai_suggestion_reviewed", "workflow", &row.WorkflowID, map[string]any{"suggestion_id": row.ID, "version_id": row.VersionID, "decision": input.Decision})
	respond.Ok(w, row)
}
