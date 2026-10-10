// Package run defines the v2.2 version-bound formal execution contract.
package run

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/trial"
)

const ContractVersion = "run.v1"

type Input struct {
	VersionID string       `json:"version_id"`
	Confirmed bool         `json:"confirmed"`
	Origins   []string     `json:"allowed_origins"`
	Budget    trial.Budget `json:"budget"`
}

func (i Input) Trial(revision int) trial.Input {
	return trial.Input{ExpectedRevision: revision, Mode: "live", Confirmed: i.Confirmed, Origins: i.Origins, Budget: i.Budget}
}
func (i Input) Validate() error {
	if _, err := uuid.Parse(i.VersionID); err != nil {
		return errors.New("published version_id required")
	}
	// Validate confirmation/origins using the shared trial contract, with its
	// small preview budget. Formal limits are independently bounded below.
	check := i.Trial(1)
	check.Budget = trial.Budget{Seconds: 5, Pages: 1, Records: 1}
	if err := check.Validate(); err != nil {
		return err
	}
	b := i.Budget
	if b.Seconds < 5 || b.Seconds > 900 || b.Pages < 1 || b.Pages > 100 || b.Records < 1 || b.Records > 1000 || b.Details < 0 || b.Details > 100 {
		return errors.New("formal budget requires seconds 5..900, pages 1..100, records 1..1000, details 0..100")
	}
	return nil
}
func Terminal(status string) bool { return status != "queued" && status != "running" }

type Origin struct {
	DocumentID  string `json:"document_id"`
	RecordIndex int    `json:"record_index"`
	URL         string `json:"source_url"`
	Stage       string `json:"stage"`
}

// Select preserves candidate ordering and its document/local-index provenance.
func Select(docs []trial.Document, config output.Config) (extraction.Result, []Origin) {
	r := extraction.Result{Interpreter: extraction.InterpreterVersion, StepID: config.StepID, Stage: config.Stage, Records: []extraction.Record{}}
	origins := []Origin{}
	for _, doc := range docs {
		if doc.StepID != config.StepID || doc.Stage != config.Stage {
			continue
		}
		r.Truncated = r.Truncated || doc.Result.Truncated
		for _, record := range doc.Result.Records {
			if doc.OutputIndices != nil {
				keep := false
				for _, index := range *doc.OutputIndices {
					if index == record.Index {
						keep = true
						break
					}
				}
				if !keep {
					continue
				}
			}
			origins = append(origins, Origin{doc.ID, record.Index, doc.URL, doc.Stage})
			record.Index = len(r.Records)
			r.Records = append(r.Records, record)
		}
	}
	return r, origins
}

type Write struct {
	Origin
	RecordID      string   `json:"record_id"`
	ObservationID string   `json:"observation_id"`
	Decision      string   `json:"decision"`
	Revision      int      `json:"record_revision"`
	ChangedFields []string `json:"changed_fields"`
	ValuesJSON    string   `json:"values_json"`
}
type Summary struct {
	trial.Summary
	Committed bool           `json:"committed"`
	Counts    map[string]int `json:"counts"`
	Writes    []Write        `json:"writes"`
}

func Empty(s trial.Summary) Summary {
	s.DryRun = false
	return Summary{Summary: s, Counts: map[string]int{"created": 0, "updated": 0, "unchanged": 0}, Writes: []Write{}}
}

// Output values may be truncated but invalid candidates or conflicting keys are
// never written. Limited successful coverage remains limited after persistence.
func Writable(s trial.Summary, p output.Preview) bool {
	if (s.Status != "succeeded" && s.Status != "limited") || s.Output == nil || s.FailedStep != "" || s.FailedStage != "" || len(p.Rows) == 0 || !p.Compatibility.Compatible || len(p.Compatibility.Issues) > 0 {
		return false
	}
	for _, row := range p.Rows {
		if len(row.Issues) > 0 || row.Decision != "created" && row.Decision != "updated" && row.Decision != "unchanged" {
			return false
		}
	}
	return true
}
func Decode(raw string, out any) error {
	// Persisted JSONB must never round int64 values through float64.
	var value json.RawMessage = []byte(raw)
	d := json.NewDecoder(bytes.NewReader(value))
	d.UseNumber()
	return d.Decode(out)
}
