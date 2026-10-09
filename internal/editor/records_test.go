package editor

import "testing"

func TestRecordPlanBudgetsAndRelativeFields(t *testing.T) {
	plan := RecordPlan{Mode: "repeated", Locator: &Locator{Strategy: "css", Expression: ".item"}, MaxRecords: 10, Fields: []FieldRule{{Name: "title", Locator: &Locator{Strategy: "xpath", Expression: ".//h2"}, Extract: "text"}}}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	plan.Fields[0].Locator.Expression = "//h2"
	if plan.Validate() == nil {
		t.Fatal("absolute XPath accepted for repeated field")
	}
	plan.Fields[0].Locator.Expression = ".//h2"
	plan.MaxRecords = 21
	if plan.Validate() == nil {
		t.Fatal("record budget ignored")
	}
}

func TestRecordPlanDetailReturnAndAttributeLimits(t *testing.T) {
	plan := RecordPlan{Mode: "single", MaxRecords: 1, Fields: []FieldRule{}, Detail: &DetailRule{Locator: &Locator{Strategy: "css", Expression: "a"}, ReturnStrategy: "back", MaxDetails: 3}}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	plan.Detail.ReturnStrategy = ""
	if plan.Validate() == nil {
		t.Fatal("implicit list return accepted")
	}
	plan.Detail.ReturnStrategy = "back"
	plan.Fields = []FieldRule{{Name: "password", Extract: "attribute", Attribute: "value"}}
	if plan.Validate() == nil {
		t.Fatal("input value attribute accepted")
	}
}
