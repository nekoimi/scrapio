// Package repair defines an offline repair handoff, never a run replay.
package repair

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/trial"
)

type Input struct {
	RunID            string `json:"run_id"`
	DocumentID       string `json:"document_id"`
	Mode             string `json:"mode"`
	ExpectedRevision int    `json:"expected_revision"`
	Confirmed        bool   `json:"confirmed"`
}

func (i Input) Validate() error {
	if _, err := uuid.Parse(i.RunID); err != nil {
		return errors.New("run_id required")
	}
	if i.DocumentID != "" {
		if _, err := uuid.Parse(i.DocumentID); err != nil {
			return errors.New("document_id invalid")
		}
	}
	if (i.Mode != "continue" && i.Mode != "fork") || i.ExpectedRevision < 1 || !i.Confirmed {
		return errors.New("confirmed mode and expected_revision required")
	}
	return nil
}

type Context struct {
	ID                            string   `json:"repair_id"`
	SourceCollectorID             string   `json:"source_collector_id"`
	TargetCollectorID             string   `json:"target_collector_id"`
	RunID                         string   `json:"run_id"`
	VersionID                     string   `json:"version_id"`
	VersionNumber                 int      `json:"version_number"`
	CurrentPublishedVersionID     *string  `json:"current_published_version_id"`
	CurrentPublishedVersionNumber int      `json:"current_published_version_number"`
	TargetRevision                int      `json:"target_revision"`
	PendingDraft                  bool     `json:"pending_draft"`
	Archived                      bool     `json:"archived"`
	Mode                          string   `json:"mode"`
	StepID                        string   `json:"step_id"`
	Stage                         string   `json:"stage"`
	Reason                        string   `json:"reason"`
	FailedStepID                  string   `json:"failed_step_id"`
	FailedStage                   string   `json:"failed_stage"`
	DocumentID                    string   `json:"document_id"`
	CaptureID                     string   `json:"capture_id"`
	Evidence                      string   `json:"evidence_status"`
	Format                        string   `json:"format"`
	FieldKeys                     []string `json:"field_keys"`
	StepCompatible                bool     `json:"step_compatible"`
}

func Eligible(status string) bool { return status == "failed" || status == "partial" }

// Evidence distinguishes an unavailable raw document from an intact empty match.
func Evidence(d *table.V22RunDocument, metadata *trial.Document) string {
	if d == nil {
		return "missing"
	}
	if metadata == nil {
		return "corrupt"
	}
	if metadata.ID != d.Id {
		return "corrupt"
	}
	if d.Content == "" {
		return "missing"
	}
	if len(d.Content) > capture.MaxBytes {
		return "too_large"
	}
	if metadata.ContentHash == "" || metadata.ContentHash != capture.Hash([]byte(d.Content)) {
		return "corrupt"
	}
	if metadata.Format != "html" && metadata.Format != "json" {
		return "unsupported"
	}
	return "available"
}

func Fields(d trial.Document) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range d.Result.Records {
		for _, f := range r.Fields {
			if !f.Valid && f.Key != "" && !seen[f.Key] {
				out = append(out, f.Key)
				seen[f.Key] = true
				if len(out) == 64 {
					return out
				}
			}
		}
	}
	return out
}

func Compatible(raw string, stepID, stage, format string) bool {
	var root struct {
		Steps []struct {
			ID     string `json:"step_id"`
			Type   string `json:"type"`
			Config struct {
				Detail *json.RawMessage `json:"detail"`
			} `json:"config"`
		} `json:"steps"`
	}
	if json.Unmarshal([]byte(raw), &root) != nil {
		return false
	}
	for _, s := range root.Steps {
		if s.ID == stepID {
			return s.Type == "json_records" && stage == "list" && format == "json" || s.Type == "record_set" && (stage == "list" || stage == "detail" && s.Config.Detail != nil) && format == "html"
		}
	}
	return false
}

// Fork keeps the exact historical rule values (including large numbers), but
// output confirmation is collector-scoped and must be performed anew.
func ForkDefinition(raw string) (string, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &root) != nil || root == nil {
		return "", errors.New("invalid frozen definition")
	}
	delete(root, "output")
	next, err := json.Marshal(root)
	return string(next), err
}
func ForkName(name string) string {
	r := []rune(strings.TrimSpace(name))
	const suffix = " (修复副本)"
	if len(r) > 160-len([]rune(suffix)) {
		r = r[:160-len([]rune(suffix))]
	}
	return string(r) + suffix
}
