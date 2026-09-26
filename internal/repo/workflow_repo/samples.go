package workflow_repo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/repo/record_repo"
	"github.com/nekoimi/scrapio/internal/workflow"
)

type SampleInput struct {
	VersionID       int64  `json:"version_id"`
	Source          string `json:"source"`
	PageRole        string `json:"page_role,omitempty"`
	DocumentID      *int64 `json:"document_id,omitempty"`
	PageURL         string `json:"page_url,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Content         string `json:"content,omitempty"`
	Note            string `json:"note,omitempty"`
	ExpectedOutcome string `json:"expected_outcome,omitempty"`
	ExpectedError   string `json:"expected_error,omitempty"`
}

type SamplePreview struct {
	DryRun          bool                          `json:"dry_run"`
	Passed          bool                          `json:"passed"`
	VersionID       int64                         `json:"version_id"`
	SampleID        int64                         `json:"sample_id,omitempty"`
	Source          string                        `json:"source"`
	FetchedLive     bool                          `json:"fetched_live"`
	PageRole        string                        `json:"page_role"`
	Discovered      []string                      `json:"discovered_urls,omitempty"`
	NextURL         string                        `json:"next_url,omitempty"`
	Steps           []workflow.RecordStep         `json:"steps"`
	Decisions       []record_repo.PreviewDecision `json:"decisions"`
	Error           string                        `json:"error,omitempty"`
	ExpectedOutcome string                        `json:"expected_outcome"`
	ActualOutcome   string                        `json:"actual_outcome"`
	ActualError     string                        `json:"actual_error,omitempty"`
}

const (
	SampleSuccess   = "success"
	SampleError     = "error"
	SampleEmptyList = "empty_list"
)

func sampleExpectation(outcome, expectedError, role string) (string, error) {
	if outcome == "" {
		outcome = SampleSuccess
	}
	switch outcome {
	case SampleSuccess:
		if expectedError != "" {
			return "", errors.New("expected_error requires error outcome")
		}
	case SampleError:
		if strings.TrimSpace(expectedError) == "" || len(expectedError) > 500 {
			return "", errors.New("error outcome requires expected_error of 1–500 characters")
		}
	case SampleEmptyList:
		if role != "list" || expectedError != "" {
			return "", errors.New("empty_list outcome requires list role and no expected_error")
		}
	default:
		return "", errors.New("expected_outcome must be success, error or empty_list")
	}
	return outcome, nil
}

func applyExpectation(result *SamplePreview, outcome, expectedError, actual string) {
	result.ActualOutcome = actual
	result.ExpectedOutcome = outcome
	switch outcome {
	case SampleSuccess:
		result.Passed = actual == SampleSuccess
		if !result.Passed && result.Error == "" {
			result.Error = "expected successful extraction"
		}
	case SampleEmptyList:
		result.Passed = actual == SampleEmptyList
		if !result.Passed && result.Error == "" {
			result.Error = "expected an empty list without next page"
		}
	case SampleError:
		result.ActualError = result.Error
		result.Passed = actual == SampleError && strings.Contains(result.ActualError, expectedError)
		if result.Passed {
			result.Error = ""
		} else {
			result.Error = fmt.Sprintf("expected error containing %q; actual: %s", expectedError, result.ActualError)
		}
	}
}

func SaveSample(input SampleInput) (*table.WorkflowSample, error) {
	if input.PageRole == "" {
		input.PageRole = "trigger"
	}
	if input.VersionID <= 0 || (input.Source != "paste" && input.Source != "document" && input.Source != "live") {
		return nil, errors.New("version_id and valid source are required")
	}
	if input.PageRole != "trigger" && input.PageRole != "list" && input.PageRole != "detail" {
		return nil, errors.New("page_role must be trigger, list or detail")
	}
	var err error
	input.ExpectedOutcome, err = sampleExpectation(input.ExpectedOutcome, input.ExpectedError, input.PageRole)
	if err != nil {
		return nil, err
	}
	if len(input.Content) == 0 || len(input.Content) > 10<<20 {
		return nil, errors.New("sample content must be 1–10485760 bytes")
	}
	if input.ContentType != "html" && input.ContentType != "json" {
		return nil, errors.New("content_type must be html or json")
	}
	if len(input.PageURL) > 4096 || len(input.Note) > 2000 {
		return nil, errors.New("sample metadata is too large")
	}
	if input.Source == "document" && input.DocumentID == nil {
		return nil, errors.New("document_id is required")
	}
	if input.Source != "document" && input.DocumentID != nil {
		return nil, errors.New("document_id only applies to document samples")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, err
	}
	defer s.Rollback()
	locked, err := s.QueryString("SELECT v.id,v.status,v.workflow_id FROM workflow_versions v WHERE v.id=? FOR UPDATE", input.VersionID)
	if err != nil {
		return nil, err
	}
	if len(locked) == 0 || locked[0]["status"] != VersionDraft {
		return nil, errors.New("samples can only be added to a draft version")
	}
	if input.Source == "document" {
		rows, err := s.QueryString(`SELECT d.id,d.document_type,d.content FROM documents d JOIN crawl_tasks t ON t.id=d.task_id JOIN workflow_runs r ON r.id=t.run_id WHERE d.id=? AND r.workflow_id=?`, *input.DocumentID, locked[0]["workflow_id"])
		if err != nil || len(rows) == 0 {
			return nil, errors.New("document does not belong to this workflow")
		}
		if rows[0]["document_type"] != input.ContentType || rows[0]["content"] != input.Content {
			return nil, errors.New("document snapshot differs from source")
		}
	}
	hash := sha256.Sum256([]byte(input.Content))
	row := &table.WorkflowSample{WorkflowVersionId: input.VersionID, Source: input.Source, PageRole: input.PageRole, DocumentId: input.DocumentID, PageURL: input.PageURL, ContentType: input.ContentType, Content: input.Content, ContentHash: hex.EncodeToString(hash[:]), Note: input.Note, ExpectedOutcome: input.ExpectedOutcome, ExpectedError: input.ExpectedError, CreatedAt: time.Now()}
	if _, err := s.Insert(row); err != nil {
		return nil, err
	}
	if err := s.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func ListSamples(versionID int64) ([]table.WorkflowSample, error) {
	rows := make([]table.WorkflowSample, 0)
	err := db.Instance().Where("workflow_version_id=?", versionID).Asc("id").Find(&rows)
	for i := range rows {
		rows[i].Content = ""
	}
	return rows, err
}

func GetSample(id int64) (*table.WorkflowSample, error) {
	row := new(table.WorkflowSample)
	has, err := db.Instance().ID(id).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, errors.New("sample not found")
	}
	return row, nil
}

func DeleteSample(id int64) error {
	if id <= 0 {
		return errors.New("sample id is required")
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	rows, err := s.QueryString("SELECT v.status FROM workflow_versions v JOIN workflow_samples x ON x.workflow_version_id=v.id WHERE x.id=? FOR UPDATE OF v", id)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("sample not found")
	}
	if rows[0]["status"] != VersionDraft {
		return errors.New("published samples cannot be removed")
	}
	if _, err := s.ID(id).Delete(new(table.WorkflowSample)); err != nil {
		return err
	}
	return s.Commit()
}

func PreviewSample(versionID int64, sample *table.WorkflowSample) (SamplePreview, error) {
	return PreviewSampleWithKeys(versionID, sample, nil)
}

func PreviewSampleWithKeys(versionID int64, sample *table.WorkflowSample, idempotencyKeys []string) (SamplePreview, error) {
	return previewSampleWithKeys(versionID, sample, idempotencyKeys, false)
}

func PreviewSampleForComparison(versionID int64, sample *table.WorkflowSample) (SamplePreview, error) {
	return previewSampleWithKeys(versionID, sample, nil, true)
}

func previewSampleWithKeys(versionID int64, sample *table.WorkflowSample, idempotencyKeys []string, allowSameWorkflow bool) (SamplePreview, error) {
	result := SamplePreview{DryRun: true, VersionID: versionID, Source: sample.Source, SampleID: sample.Id, PageRole: sample.PageRole, FetchedLive: false, Steps: []workflow.RecordStep{}, Decisions: []record_repo.PreviewDecision{}}
	outcome, expectationErr := sampleExpectation(sample.ExpectedOutcome, sample.ExpectedError, sample.PageRole)
	if expectationErr != nil {
		return result, expectationErr
	}
	result.ExpectedOutcome = outcome
	if !allowSameWorkflow && sample.WorkflowVersionId != versionID {
		return result, errors.New("sample belongs to another version")
	}
	version, has, err := GetVersion(versionID)
	if err != nil {
		return result, err
	}
	if !has {
		return result, errors.New("workflow version not found")
	}
	if allowSameWorkflow && sample.WorkflowVersionId != versionID {
		source, exists, sourceErr := GetVersion(sample.WorkflowVersionId)
		if sourceErr != nil || !exists || source.WorkflowId != version.WorkflowId {
			return result, errors.New("sample belongs to another workflow")
		}
	}
	owner, has, err := Get(version.WorkflowId)
	if err != nil {
		return result, err
	}
	if !has || owner.DatasetId == nil {
		return result, errors.New("workflow dataset not found")
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if definition.Persistence != "records" {
		result.Error = "record dry-run requires records persistence"
		return result, nil
	}
	role := sample.PageRole
	if role == "" {
		role = "trigger"
	}
	if definition.Listing != nil {
		if role != "list" && role != "detail" {
			result.Error = "listing sample requires list or detail page_role"
			return result, nil
		}
		if role == "list" {
			listing, err := workflow.ExtractListing(*definition.Listing, workflow.FetchResult{HTML: sample.Content, FinalURL: sample.PageURL}, definition.Trigger.URL)
			if err != nil {
				result.Error = err.Error()
				applyExpectation(&result, outcome, sample.ExpectedError, SampleError)
				return result, nil
			}
			result.PageRole = role
			result.Discovered = listing.Details
			result.NextURL = listing.NextURL
			result.Steps = append(result.Steps, workflow.RecordStep{Candidate: 0, Node: "listing", Type: "discover", Values: map[string]any{"details": listing.Details, "next_url": listing.NextURL}})
			actual := SampleSuccess
			if len(listing.Details) == 0 && listing.NextURL == "" {
				actual = SampleEmptyList
			}
			applyExpectation(&result, outcome, sample.ExpectedError, actual)
			return result, nil
		}
	} else if role != "trigger" {
		result.Error = "single-page sample requires trigger page_role"
		return result, nil
	}
	schema, err := record_repo.DatasetSchema(*owner.DatasetId)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	document := workflow.FetchResult{Adapter: "sample", FinalURL: sample.PageURL, ContentType: sample.ContentType}
	if sample.ContentType == "json" {
		document.JSON = sample.Content
	} else {
		document.HTML = sample.Content
	}
	result.Steps = append(result.Steps, workflow.RecordStep{Candidate: 0, Node: "acquire", Type: "acquire", Values: map[string]any{"source": sample.Source, "page_url": sample.PageURL, "content_type": sample.ContentType, "content_hash": sample.ContentHash}})
	values, steps, err := workflow.TraceRecordCandidates(definition, document, role, schema)
	result.Steps = append(result.Steps, steps...)
	if err != nil {
		if len(values) > 0 {
			result.Decisions, _ = record_repo.PreviewBatch(*owner.DatasetId, schema, values)
		}
		result.Decisions = append(result.Decisions, record_repo.PreviewDecision{Index: len(values), Decision: "invalid", Reason: err.Error()})
		result.Error = err.Error()
		applyExpectation(&result, outcome, sample.ExpectedError, SampleError)
		return result, nil
	}
	if len(idempotencyKeys) > 0 && len(idempotencyKeys) != len(values) {
		return result, errors.New("idempotency_keys must match candidate count")
	}
	decisions, err := record_repo.PreviewBatch(*owner.DatasetId, schema, values)
	if err != nil {
		return result, err
	}
	result.Decisions = decisions
	for i, key := range idempotencyKeys {
		conflict, err := record_repo.PreviewIdempotency(*owner.DatasetId, key, values[i], schema)
		if err != nil {
			return result, err
		}
		if conflict {
			result.Decisions[i].Decision = "conflict"
			result.Decisions[i].Reason = "idempotency key belongs to a different candidate"
		}
	}
	for _, decision := range result.Decisions {
		if decision.Decision == "invalid" || decision.Decision == "conflict" {
			result.Error = fmt.Sprintf("candidate[%d]: %s: %s", decision.Index, decision.Decision, decision.Reason)
			break
		}
	}
	actual := SampleSuccess
	if result.Error != "" {
		actual = SampleError
	}
	applyExpectation(&result, outcome, sample.ExpectedError, actual)
	return result, nil
}

func CheckSamples(versionID int64) ([]SamplePreview, error) {
	var samples []table.WorkflowSample
	if err := db.Instance().Where("workflow_version_id=?", versionID).Asc("id").Find(&samples); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, &workflow.DefinitionError{Cause: errors.New("samples: at least one sample is required for records publication")}
	}
	checks := make([]SamplePreview, 0, len(samples))
	var firstError error
	version, has, err := GetVersion(versionID)
	if err != nil || !has {
		return nil, errors.New("workflow version not found")
	}
	definition, err := workflow.ParseExecutableDefinition(version.Definition)
	if err != nil {
		return nil, err
	}
	listCount, detailCount, discoveredCount, successCount := 0, 0, 0, 0
	for _, sample := range samples {
		preview, err := PreviewSample(versionID, &sample)
		if err != nil {
			preview.Passed = false
			preview.Error = err.Error()
			if firstError == nil {
				firstError = err
			}
		}
		checks = append(checks, preview)
		if preview.Passed && preview.ExpectedOutcome == SampleSuccess {
			successCount++
		}
		if preview.Passed && preview.PageRole == "list" && preview.ExpectedOutcome == SampleSuccess {
			listCount++
			discoveredCount += len(preview.Discovered)
		}
		if preview.Passed && preview.PageRole == "detail" && preview.ExpectedOutcome == SampleSuccess {
			detailCount++
		}
		if !preview.Passed && firstError == nil {
			firstError = &workflow.DefinitionError{Cause: fmt.Errorf("samples[%d]: %s", sample.Id, strings.TrimSpace(preview.Error))}
		}
	}
	if successCount == 0 && firstError == nil {
		firstError = &workflow.DefinitionError{Cause: errors.New("samples: at least one successful output sample is required")}
	}
	if definition.Listing != nil && (listCount == 0 || detailCount == 0 || discoveredCount == 0) && firstError == nil {
		firstError = &workflow.DefinitionError{Cause: errors.New("samples: listing publication requires a list sample with details and a detail sample")}
	}
	return checks, firstError
}
