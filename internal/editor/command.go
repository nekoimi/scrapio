package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
)

var Actions = []string{"navigate", "back", "forward", "refresh", "click", "input", "wait", "scroll"}

type Locator struct {
	Strategy   string `json:"strategy"`
	Expression string `json:"expression"`
}
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Command struct {
	PageStateID      string    `json:"page_state_id"`
	ExpectedRevision int       `json:"expected_revision"`
	Type             string    `json:"type"`
	Locator          *Locator  `json:"locator,omitempty"`
	Position         *Position `json:"position,omitempty"`
	Value            string    `json:"value,omitempty"`
	TimeoutMS        int       `json:"timeout_ms"`
	Confirmed        bool      `json:"confirmed"`
	Record           bool      `json:"record"`
	StepID           string    `json:"step_id,omitempty"`
}

func (c *Command) Validate() error {
	if c.TimeoutMS == 0 {
		c.TimeoutMS = 10000
	}
	if c.PageStateID == "" || len(c.PageStateID) > 256 || c.ExpectedRevision < 1 || c.TimeoutMS < 100 || c.TimeoutMS > 30000 {
		return errors.New("page_state_id, expected_revision and bounded timeout_ms are required")
	}
	if len(c.Value) > 4096 || len(c.StepID) > 128 {
		return errors.New("command value or step_id exceeds limit")
	}
	if c.Locator != nil && (c.Locator.Strategy != "css" && c.Locator.Strategy != "xpath" || c.Locator.Expression == "" || len(c.Locator.Expression) > 2048) {
		return errors.New("locator requires css/xpath and a bounded expression")
	}
	if c.Position != nil && (c.Type != "click" || c.Locator != nil || math.IsNaN(c.Position.X) || math.IsNaN(c.Position.Y) || math.IsInf(c.Position.X, 0) || math.IsInf(c.Position.Y, 0) || c.Position.X < 0 || c.Position.Y < 0 || c.Position.X > 8192 || c.Position.Y > 8192) {
		return errors.New("invalid click position")
	}
	switch c.Type {
	case "navigate":
		u, err := url.Parse(c.Value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return errors.New("navigate requires an http(s) URL without credentials")
		}
	case "click", "input":
		if !c.Confirmed {
			return errors.New("click/input requires explicit confirmation")
		}
		if c.Locator == nil && (c.Type != "click" || c.Position == nil) {
			return errors.New("click/input requires a locator or click coordinates")
		}
	case "scroll":
		n, err := strconv.Atoi(c.Value)
		if err != nil || n < -100000 || n > 100000 {
			return errors.New("scroll requires an integer between -100000 and 100000")
		}
	case "wait":
		if c.Locator == nil {
			n, err := strconv.Atoi(c.Value)
			if err != nil || n < 0 || n > c.TimeoutMS {
				return errors.New("wait duration must fit timeout_ms")
			}
		}
	case "back", "forward", "refresh":
	default:
		return errors.New("unsupported editor action")
	}
	if c.Record && c.StepID == "" {
		return errors.New("recording requires step_id")
	}
	return nil
}

func (c Command) Fingerprint() string {
	raw, _ := json.Marshal(c)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Rejected/uncertain input text must not be stored in the command journal.
func (c Command) Journal() string {
	if c.Type == "input" {
		c.Value = ""
	}
	raw, _ := json.Marshal(c)
	return string(raw)
}

func (c Command) Step() map[string]any {
	config := map[string]any{"action": c.Type, "timeout_ms": c.TimeoutMS}
	if c.Locator != nil {
		config["locator"] = c.Locator
	}
	if c.Value != "" {
		config["value"] = c.Value
	}
	kind := "action"
	if c.Type == "navigate" {
		kind = "navigate"
		config = map[string]any{"url": c.Value, "timeout_ms": c.TimeoutMS}
	}
	return map[string]any{"step_id": c.StepID, "type": kind, "config": config}
}

func FromStep(step map[string]any) (Command, error) {
	c := Command{PageStateID: "checkpoint", ExpectedRevision: 1, TimeoutMS: 10000, Confirmed: true}
	c.StepID, _ = step["step_id"].(string)
	config, ok := step["config"].(map[string]any)
	if !ok {
		return c, errors.New("step config must be an object")
	}
	kind, _ := step["type"].(string)
	if kind == "navigate" {
		c.Type = "navigate"
		c.Value, _ = config["url"].(string)
	} else if kind == "action" {
		c.Type, _ = config["action"].(string)
		if raw, exists := config["value"]; exists {
			var valid bool
			c.Value, valid = raw.(string)
			if !valid {
				return c, errors.New("action value must be a string")
			}
		}
	} else {
		return c, errors.New("checkpoint only supports navigation/action steps")
	}
	if raw, exists := config["timeout_ms"]; exists {
		value, ok := raw.(float64)
		if !ok || value != math.Trunc(value) || value < 100 || value > 30000 {
			return c, errors.New("timeout_ms must be integer")
		}
		c.TimeoutMS = int(value)
	}
	if raw, exists := config["locator"]; exists {
		data, _ := json.Marshal(raw)
		c.Locator = new(Locator)
		if err := json.Unmarshal(data, c.Locator); err != nil {
			return c, err
		}
	}
	if strings.TrimSpace(c.StepID) == "" {
		return c, errors.New("step_id is required")
	}
	return c, c.Validate()
}
