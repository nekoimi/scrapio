package editor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommandConfirmationAndInputJournal(t *testing.T) {
	c := Command{Type: "input", Value: "private text", Locator: &Locator{Strategy: "css", Expression: "#search"}, PageStateID: "page:1", ExpectedRevision: 1}
	if c.Validate() == nil {
		t.Fatal("unconfirmed input accepted")
	}
	c.Confirmed = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.Journal(), c.Value) {
		t.Fatal("input text leaked into durable command journal")
	}
	other := c
	other.Value = "different"
	if c.Fingerprint() == other.Fingerprint() {
		t.Fatal("input changes did not affect idempotency fingerprint")
	}
}

func TestCheckpointActionRoundTripAndUnsupportedSteps(t *testing.T) {
	c := Command{StepID: "step-1", Type: "click", Locator: &Locator{Strategy: "xpath", Expression: "//button[@id='more']"}, TimeoutMS: 15000}
	raw, _ := json.Marshal(c.Step())
	var step map[string]any
	_ = json.Unmarshal(raw, &step)
	decoded, err := FromStep(step)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != c.Type || decoded.Locator.Expression != c.Locator.Expression || decoded.TimeoutMS != 15000 {
		t.Fatalf("lost replay parameters: %+v", decoded)
	}
	step["type"] = "extract"
	if _, err := FromStep(step); err == nil {
		t.Fatal("checkpoint accepted unsupported extraction step")
	}
}

func TestCommandBoundsAndURLCredentials(t *testing.T) {
	c := Command{PageStateID: "page:1", ExpectedRevision: 1, Type: "navigate", Value: "https://user:password@example.test/"}
	if c.Validate() == nil {
		t.Fatal("embedded credentials accepted")
	}
	c.Type = "scroll"
	c.Value = "100001"
	if c.Validate() == nil {
		t.Fatal("unbounded scroll accepted")
	}
	c.Type = "wait"
	c.Value = "15000"
	c.TimeoutMS = 1000
	if c.Validate() == nil {
		t.Fatal("wait exceeds timeout")
	}
}
