// Package trial executes bounded v2.2 dry runs. It has no record writer or plugin dispatcher.
package trial

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/url"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
)

type Budget struct {
	Seconds int `json:"seconds"`
	Pages   int `json:"pages"`
	Records int `json:"records"`
	Details int `json:"details"`
}
type InputRef struct {
	StepID    string `json:"step_id"`
	Stage     string `json:"stage"`
	CaptureID string `json:"capture_id"`
}
type Input struct {
	ExpectedRevision int        `json:"expected_revision"`
	Mode             string     `json:"mode"`
	Confirmed        bool       `json:"confirmed"`
	Origins          []string   `json:"allowed_origins"`
	Budget           Budget     `json:"budget"`
	Inputs           []InputRef `json:"inputs"`
}

func (i Input) Validate() error {
	if i.ExpectedRevision < 1 || i.Mode != "live" && i.Mode != "offline" || i.Budget.Seconds < 5 || i.Budget.Seconds > 120 || i.Budget.Pages < 1 || i.Budget.Pages > 10 || i.Budget.Records < 1 || i.Budget.Records > 20 || i.Budget.Details < 0 || i.Budget.Details > 5 {
		return errors.New("revision, live/offline mode and bounded budget required")
	}
	if i.Mode == "live" && (!i.Confirmed || len(i.Inputs) > 0 || len(i.Origins) < 1 || len(i.Origins) > 5) {
		return errors.New("live trial requires explicit confirmation and 1..5 origins, without offline inputs")
	}
	if i.Mode == "offline" && (len(i.Inputs) < 1 || len(i.Inputs) > 6 || len(i.Origins) > 0) {
		return errors.New("offline trial requires 1..6 fixed inputs, without live origins")
	}
	seen := map[string]bool{}
	for _, ref := range i.Inputs {
		if _, err := uuid.Parse(ref.CaptureID); err != nil {
			return errors.New("valid capture ID required")
		}
		key := ref.StepID + ":" + ref.Stage
		if ref.StepID == "" || len(ref.StepID) > 128 || ref.CaptureID == "" || ref.Stage != "list" && ref.Stage != "detail" || seen[key] {
			return errors.New("unique step/role input required")
		}
		seen[key] = true
	}
	for _, origin := range i.Origins {
		u, err := capture.ValidateURL(origin)
		if err != nil || origin != u.Scheme+"://"+u.Host {
			return errors.New("allowed origins must be exact http(s) origins without paths")
		}
	}
	return nil
}

type Step struct {
	ID     string
	Kind   string
	Action editor.Command
	Plan   extraction.Plan
}
type Plan struct {
	URL       string
	EntryType string
	Request   capture.HTTPRequest
	Steps     []Step
	Output    output.Config
}

func Compile(raw []byte, entryType string) (Plan, error) {
	var root struct {
		Version int              `json:"definition_version"`
		URL     string           `json:"entry_url"`
		Request json.RawMessage  `json:"http_request"`
		Steps   []map[string]any `json:"steps"`
		Output  json.RawMessage  `json:"output"`
	}
	p := Plan{EntryType: entryType}
	if json.Unmarshal(raw, &root) != nil || root.Version != 1 || len(root.Steps) < 1 || len(root.Steps) > 50 {
		return p, errors.New("trial requires 1..50 supported saved steps")
	}
	if _, err := capture.ValidateURL(root.URL); err != nil {
		return p, err
	}
	p.URL = root.URL
	var err error
	p.Output, err = output.DecodeConfig(root.Output)
	if err != nil {
		return p, errors.New("confirm output configuration before trial")
	}
	if entryType == "json" {
		p.Request, err = capture.DecodeRequest(root.Request)
		if err != nil {
			return p, err
		}
	} else if entryType != "web" {
		return p, errors.New("unsupported entry type")
	}
	seen := map[string]bool{}
	outputFound := false
	for _, rawStep := range root.Steps {
		id, _ := rawStep["step_id"].(string)
		kind, _ := rawStep["type"].(string)
		if id == "" || len(id) > 128 || seen[id] {
			return p, errors.New("unique step IDs required")
		}
		seen[id] = true
		s := Step{ID: id, Kind: kind}
		switch kind {
		case "action", "navigate":
			if entryType != "web" {
				return p, fmt.Errorf("step %s: JSON entrance cannot execute browser actions", id)
			}
			s.Action, err = editor.FromStep(rawStep)
		case "record_set", "json_records":
			if (entryType == "json") != (kind == "json_records") {
				return p, fmt.Errorf("step %s: record kind does not match entrance", id)
			}
			s.Plan, err = extraction.PlanFromDefinition(raw, id)
			if id == p.Output.StepID {
				outputFound = true
				if p.Output.Stage == "detail" && (kind != "record_set" || s.Plan.HTML.Detail == nil) {
					return p, errors.New("output detail role requires a detail path")
				}
			}
		default:
			return p, fmt.Errorf("step %s: unsupported trial step %s", id, kind)
		}
		if err != nil {
			return p, fmt.Errorf("step %s: %w", id, err)
		}
		p.Steps = append(p.Steps, s)
	}
	if !outputFound {
		return p, errors.New("output extraction step missing")
	}
	return p, nil
}
func Allowed(raw string, origins []string) bool {
	u, err := capture.ValidateURL(raw)
	if err != nil {
		return false
	}
	for _, o := range origins {
		if o == u.Scheme+"://"+u.Host {
			return true
		}
	}
	return false
}
func ValidateScope(p Plan, i Input) error {
	if i.Mode == "offline" {
		return nil
	}
	if !Allowed(p.URL, i.Origins) {
		return errors.New("entrance outside confirmed origins")
	}
	for _, s := range p.Steps {
		if s.Action.Type == "navigate" && !Allowed(s.Action.Value, i.Origins) {
			return fmt.Errorf("step %s: navigation outside confirmed origins", s.ID)
		}
	}
	return nil
}
func Origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
