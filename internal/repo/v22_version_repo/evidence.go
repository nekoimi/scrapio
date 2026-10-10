package v22_version_repo

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repo/v22_collector_repo"
	"github.com/nekoimi/scrapio/internal/sample"
	"github.com/nekoimi/scrapio/internal/trial"
	"xorm.io/xorm"
)

type sampleIdentity struct {
	ID           string `json:"sample_id"`
	Revision     int    `json:"revision"`
	StepID       string `json:"step_id"`
	Stage        string `json:"stage"`
	Kind         string `json:"kind"`
	CaptureID    string `json:"capture_id"`
	ContentHash  string `json:"content_hash"`
	ExpectedHash string `json:"expected_hash"`
}
type manifest struct {
	Revision       int              `json:"revision"`
	DefinitionHash string           `json:"definition_hash"`
	SchemaHash     string           `json:"schema_hash"`
	SchemaVersion  int              `json:"schema_version"`
	OutputCheckID  string           `json:"output_check_id"`
	TrialID        string           `json:"trial_id"`
	TrialHash      string           `json:"trial_hash"`
	Samples        []sampleIdentity `json:"samples"`
}
type sampleProof struct {
	sampleIdentity
	Name       string              `json:"name"`
	Input      trial.FixedDocument `json:"input"`
	Expected   json.RawMessage     `json:"expected"`
	Result     extraction.Result   `json:"result"`
	Comparison sample.Comparison   `json:"comparison"`
}
type state struct {
	collector *table.V22Collector
	plan      trial.Plan
	schema    output.Schema
	schemaRaw string
	trial     *table.V22Trial
	input     trial.Input
	summary   trial.Summary
	samples   []table.V22Sample
	captures  map[string]table.V22Capture
	manifest  manifest
}

// Existing B02 check/confirm takes sample before collector. Lock all current
// samples in stable order first; re-read membership after collector lock to
// detect a concurrent create without reversing that lock order.
func load(s *xorm.Session, ownerID, collectorID int64, revision int, trialID string) (*state, error) {
	initial, err := s.QueryString("SELECT id FROM v22_samples WHERE owner_id=? AND collector_id=? ORDER BY id FOR UPDATE", ownerID, collectorID)
	if err != nil {
		return nil, err
	}
	if _, err = s.QueryString("SELECT id FROM v22_collectors WHERE owner_id=? AND id=? FOR UPDATE", ownerID, collectorID); err != nil {
		return nil, err
	}
	st := &state{collector: new(table.V22Collector), captures: map[string]table.V22Capture{}}
	has, err := s.Where("owner_id=? AND id=?", ownerID, collectorID).Get(st.collector)
	if err != nil {
		return nil, err
	}
	if !has || st.collector.Status == "archived" {
		return nil, ErrNotFound
	}
	if st.collector.Revision != revision {
		return nil, &v22_collector_repo.RevisionConflict{Latest: st.collector}
	}
	st.samples = []table.V22Sample{}
	if err = s.Where("owner_id=? AND collector_id=?", ownerID, collectorID).Omit("screenshot").Asc("id").Find(&st.samples); err != nil {
		return nil, err
	}
	if len(initial) != len(st.samples) {
		return nil, ErrConflict
	}
	for i, row := range st.samples {
		if initial[i]["id"] != row.Id {
			return nil, ErrConflict
		}
	}
	st.plan, err = trial.Compile([]byte(st.collector.Definition), st.collector.EntryType)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if st.plan.URL != st.collector.EntryURL {
		return nil, invalid("definition entrance differs from collector")
	}
	id, _ := strconv.ParseInt(st.plan.Output.TableID, 10, 64)
	if _, err = s.QueryString("SELECT id FROM v22_data_tables WHERE owner_id=? AND id=? FOR SHARE", ownerID, id); err != nil {
		return nil, err
	}
	target := new(table.V22DataTable)
	has, err = s.Where("owner_id=? AND id=?", ownerID, id).Get(target)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	st.schema, err = output.DecodeSchema([]byte(target.Schema))
	if err != nil {
		return nil, err
	}
	if target.SchemaHash != output.SchemaHash(st.schema) || target.SchemaHash != st.plan.Output.SchemaHash || target.SchemaVersion != st.plan.Output.SchemaVersion {
		return nil, ErrConflict
	}
	st.schemaRaw = target.Schema
	binding := new(table.V22OutputBinding)
	has, err = s.Where("owner_id=? AND collector_id=? AND table_id=? AND check_id=?", ownerID, collectorID, id, st.plan.Output.CheckID).Get(binding)
	if err != nil {
		return nil, err
	}
	if !has || publication.DefinitionHash(binding.Config) != publication.Hash(st.plan.Output) {
		return nil, invalid("output must match the confirmed binding")
	}
	if _, err = s.QueryString("SELECT id FROM v22_trials WHERE owner_id=? AND collector_id=? AND id=? FOR SHARE", ownerID, collectorID, trialID); err != nil {
		return nil, err
	}
	st.trial = new(table.V22Trial)
	has, err = s.Where("owner_id=? AND collector_id=? AND id=?", ownerID, collectorID, trialID).Get(st.trial)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if json.Unmarshal([]byte(st.trial.Input), &st.input) != nil || decode([]byte(st.trial.Summary), &st.summary) != nil {
		return nil, invalid("trial evidence is corrupt")
	}
	st.manifest = manifest{Revision: revision, DefinitionHash: publication.DefinitionHash(st.collector.Definition), SchemaHash: target.SchemaHash, SchemaVersion: target.SchemaVersion, OutputCheckID: binding.CheckId, TrialID: trialID, TrialHash: publication.Hash([]any{st.trial.Input, st.trial.Summary, st.trial.DefinitionHash, st.trial.FinishedAt}), Samples: []sampleIdentity{}}
	for _, row := range st.samples {
		c := table.V22Capture{}
		has, err = s.Where("owner_id=? AND collector_id=? AND id=?", ownerID, collectorID, row.CaptureId).Get(&c)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, ErrNotFound
		}
		st.captures[row.Id] = c
		if c.Status != "succeeded" || c.ContentHash != capture.Hash([]byte(c.Content)) || row.ExpectedHash != capture.Hash(sample.CanonicalJSON([]byte(row.Expected))) {
			return nil, invalid("sample input or expectations are corrupt")
		}
		st.manifest.Samples = append(st.manifest.Samples, sampleIdentity{row.Id, row.Revision, row.StepId, row.Stage, row.Kind, row.CaptureId, c.ContentHash, row.ExpectedHash})
	}
	return st, nil
}
func evaluate(ctx context.Context, st *state, caps publication.Capabilities, acceptLimited bool) (publication.Result, []sampleProof, error) {
	result := publication.CheckCapabilities(st.plan, caps)
	if caps.DefinitionHash != st.manifest.DefinitionHash {
		return result, nil, ErrConflict
	}
	publication.Merge(&result, publication.CheckTrial(st.trial.Status, st.trial.CollectorRevision, st.trial.DefinitionHash, st.trial.FinishedAt, st.input, st.summary, st.collector.Revision, st.manifest.DefinitionHash, acceptLimited, time.Now()))
	if st.trial.CancelRequested || publication.DefinitionHash(st.trial.Definition) != st.trial.DefinitionHash || publication.DefinitionHash(st.trial.OutputSchema) != publication.Hash(st.schema) {
		result.Block("TRIAL_EVIDENCE_INVALID", "试采定义、Schema 或取消状态不匹配", "", "", "", "")
	}
	if err := trial.ValidateScope(st.plan, st.input); err != nil {
		result.Block("TRIAL_SCOPE_INVALID", "试采范围不覆盖当前入口及导航", "", "", "", "")
	}
	for _, step := range st.plan.Steps {
		if step.ID == st.plan.Output.StepID {
			compat := output.CheckCompatibility(st.schema, st.plan.Output.Mapping, output.SourceFields(step.Plan, st.plan.Output.Stage))
			for _, issue := range compat.Issues {
				result.Block(issue.Code, "字段与目标数据表不兼容", step.ID, st.plan.Output.Stage, "", issue.Source)
			}
		}
	}
	proofs := []sampleProof{}
	normal := map[string]bool{}
	missing := false
	size := 0
	for i, row := range st.samples {
		if err := ctx.Err(); err != nil {
			return result, nil, err
		}
		c := st.captures[row.Id]
		proof := sampleProof{sampleIdentity: st.manifest.Samples[i], Name: row.Name, Input: trial.FixedDocument{CaptureID: c.Id, Content: c.Content, Hash: c.ContentHash, URL: c.FinalURL, BaseURL: c.BaseURL, Format: c.Format}, Expected: json.RawMessage(row.Expected)}
		expected, err := sample.DecodeExpected([]byte(row.Expected))
		valid := err == nil && capture.Hash(sample.CanonicalJSON([]byte(row.Expected))) == row.ExpectedHash && c.Status == "succeeded" && capture.Hash([]byte(c.Content)) == c.ContentHash
		p, planErr := extraction.PlanFromDefinition([]byte(st.collector.Definition), row.StepId)
		if !valid || planErr != nil {
			result.Block("SAMPLE_INPUT_INVALID", "样例输入、预期或对应步骤无效", row.StepId, row.Stage, row.Id, "")
		} else {
			proof.Result, err = extraction.Extract(ctx, extraction.Input{Content: c.Content, Format: c.Format, URL: c.FinalURL, BaseURL: c.BaseURL, Stage: row.Stage}, p)
			if err != nil {
				result.Block("SAMPLE_EXTRACTION_FAILED", "样例无法按当前规则提取", row.StepId, row.Stage, row.Id, "")
			} else {
				proof.Comparison = sample.Compare(proof.Result, expected)
				if proof.Comparison.Status != "passed" {
					result.Block("SAMPLE_CHECK_FAILED", "样例未配置断言或未通过当前规则检查", row.StepId, row.Stage, row.Id, "")
					for _, difference := range proof.Comparison.Differences {
						result.Block(difference.Code, "样例字段或记录预期不匹配", row.StepId, row.Stage, row.Id, difference.FieldKey)
					}
				} else {
					if row.Kind == "normal" && proof.Result.ValidCount > 0 {
						normal[row.StepId+":"+row.Stage] = true
					}
				}
			}
		}
		if proof.Comparison.Status == "passed" && row.Kind == "missing_field" {
			for _, f := range expected.Fields {
				if string(f.Value) == "null" || f.Errors != nil && len(*f.Errors) > 0 {
					missing = true
				}
			}
		}
		raw, _ := json.Marshal(proof)
		size += len(raw)
		if size > 8*capture.MaxBytes {
			return result, nil, invalid("publication sample evidence exceeds 8 MiB; reduce samples or input size")
		}
		proofs = append(proofs, proof)
	}
	for _, step := range st.plan.Steps {
		if step.Kind == "record_set" || step.Kind == "json_records" {
			if !normal[step.ID+":list"] {
				result.Block("NORMAL_SAMPLE_REQUIRED", "每个提取角色至少需一个通过且有有效记录的正常样例", step.ID, "list", "", "")
			}
			if step.Plan.HTML.Detail != nil && !normal[step.ID+":detail"] {
				result.Block("NORMAL_SAMPLE_REQUIRED", "详情角色需一个通过的正常样例", step.ID, "detail", "", "")
			}
		}
	}
	if !missing {
		result.Block("MISSING_FIELD_SAMPLE_REQUIRED", "至少需一个通过并显式断言缺字段空值或错误的缺字段样例", "", "", "", "")
	}
	return result, proofs, nil
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(out)
}
