package editor

import (
	"math"
	"testing"
)

// Sample references are transient inspection input, never executable rules.
func TestInspectionSampleLimitsAndExtractionIntent(t *testing.T) {
	valid := Inspection{PageStateID: "page", ElementID: "node", SampleElementIDs: []string{"other"}, Extract: "attribute", Attribute: "data-code"}
	if err := valid.Validate("inspect"); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"a", "a"}, {""}, {"1", "2", "3", "4", "5", "6"}} {
		input := valid
		input.SampleElementIDs = ids
		if input.Validate("inspect") == nil {
			t.Fatalf("invalid samples accepted: %v", ids)
		}
	}
	valid.SampleElementIDs = nil
	valid.Attribute = ""
	if valid.Validate("inspect") == nil {
		t.Fatal("unnamed extraction attribute accepted")
	}
	valid.Extract = "script"
	if valid.Validate("inspect") == nil {
		t.Fatal("unknown extraction intent accepted")
	}
}

func TestInspectionBoundsAndExclusiveTarget(t *testing.T) {
	valid := Inspection{PageStateID: "page:1", Position: &Position{X: 10, Y: 20}}
	if err := valid.Validate("inspect"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []Inspection{
		{PageStateID: "page:1"},
		{PageStateID: "page:1", Position: &Position{X: math.NaN()}},
		{PageStateID: "page:1", ElementID: "node-1", Position: &Position{}},
		{PageStateID: "page:1", ElementID: "node-1", Limit: 51},
	} {
		if err := input.Validate("inspect"); err == nil {
			t.Fatalf("invalid inspection accepted: %+v", input)
		}
	}
}

func TestLocatorInspectionRequiresRuleAndAllowsScope(t *testing.T) {
	input := Inspection{PageStateID: "page:1"}
	if input.Validate("locator-check") == nil {
		t.Fatal("missing locator accepted")
	}
	input.Locator = &Locator{Strategy: "xpath", Expression: ".//a"}
	input.Scope = &Locator{Strategy: "css", Expression: ".item"}
	if err := input.Validate("locator-check"); err != nil {
		t.Fatal(err)
	}
	input.Scope.Strategy = "script"
	if input.Validate("locator-check") == nil {
		t.Fatal("script locator accepted")
	}
}
