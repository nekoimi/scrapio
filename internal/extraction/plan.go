package extraction

import (
	"encoding/json"
	"errors"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
)

type Plan struct {
	StepID string
	Kind   string
	JSON   capture.JSONPlan
	HTML   editor.RecordPlan
}

func PlanFromDefinition(definition []byte, stepID string) (Plan, error) {
	var raw struct {
		Version int                          `json:"definition_version"`
		Steps   []map[string]json.RawMessage `json:"steps"`
	}
	plan := Plan{StepID: stepID}
	if json.Unmarshal(definition, &raw) != nil || raw.Version != 1 || raw.Steps == nil {
		return plan, errors.New("invalid definition version or steps")
	}
	seen := map[string]bool{}
	found := false
	for _, step := range raw.Steps {
		var id, kind string
		if json.Unmarshal(step["step_id"], &id) != nil || id == "" || seen[id] {
			return plan, errors.New("step_id must be present and unique")
		}
		seen[id] = true
		if id != stepID {
			continue
		}
		found = true
		_ = json.Unmarshal(step["type"], &kind)
		plan.Kind = kind
		switch kind {
		case "json_records":
			p, err := capture.DecodePlan(step["config"])
			if err != nil {
				return plan, err
			}
			plan.JSON = p
		case "record_set":
			var parsed map[string]any
			data, _ := json.Marshal(step)
			if json.Unmarshal(data, &parsed) != nil {
				return plan, errors.New("invalid record step")
			}
			p, err := editor.RecordPlanFromStep(parsed)
			if err != nil {
				return plan, err
			}
			plan.HTML = p
		default:
			return plan, errors.New("preview requires record_set or json_records step")
		}
	}
	if !found {
		return plan, errors.New("record step not found")
	}
	return plan, nil
}
