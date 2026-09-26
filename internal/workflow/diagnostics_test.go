package workflow

import "testing"

func TestFieldDiagnosticsMissingAndConverted(t *testing.T) {
	definition := Templates()[0].Definition
	document := FetchResult{HTML: `<link rel="canonical" href="https://example.org/a"><h1>  A  </h1>`}
	rows := DiagnoseExtractNode(definition.Nodes[0], 0, document)
	if len(rows) != 3 || rows[0].MatchCount != 1 || rows[1].MatchCount != 1 || rows[2].MatchCount != 0 {
		t.Fatalf("match counts: %+v", rows)
	}
	if rows[1].Raw != "A" || rows[1].Converted != "A" || rows[2].RulePath != "nodes[0].config.fields[2]" {
		t.Fatalf("field diagnostics: %+v", rows)
	}
	missing := FetchResult{HTML: `<h1>Title</h1>`}
	_, steps, err := TraceRecordCandidates(definition, missing, "trigger", articleSchema())
	if err == nil || len(steps) == 0 || len(steps[0].Fields) == 0 || steps[0].Fields[0].MatchCount != 0 || steps[0].Fields[0].Error == "" {
		t.Fatalf("required field failure was not localized: %+v %v", steps, err)
	}
}
