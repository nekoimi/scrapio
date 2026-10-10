package publication

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/trial"
)

func trialFixture() (trial.Input, trial.Summary, time.Time) {
	now := time.Now()
	input := trial.Input{ExpectedRevision: 4, Mode: "live", Confirmed: true, Origins: []string{"https://example.com"}, Budget: trial.Budget{Seconds: 60, Pages: 3, Records: 20, Details: 1}}
	summary := trial.Summary{Status: "succeeded", Reason: "COMPLETED", Candidates: 1, Output: &output.Preview{Ready: true, Compatibility: output.Compatibility{Compatible: true}, Counts: map[string]int{"created": 1}, Rows: []output.Decision{{Decision: "created"}}}}
	return input, summary, now
}
func TestPublishRequiresFreshLiveEvidence(t *testing.T) {
	input, summary, now := trialFixture()
	finished := now.Add(-time.Hour)
	result := CheckTrial("succeeded", 4, "hash", &finished, input, summary, 4, "hash", false, now)
	if !result.Ready {
		t.Fatal(result)
	}
	for _, name := range []string{"old-revision", "changed-definition", "expired", "offline", "cancelled", "partial", "zero-candidates", "field-errors", "key-conflict", "missing-output", "empty-rows", "unknown-decision", "incompatible-schema"} {
		t.Run(name, func(t *testing.T) {
			i, s, at := trialFixture()
			revision, hash, status := 4, "hash", "succeeded"
			at = now.Add(-time.Hour)
			switch name {
			case "old-revision":
				revision = 3
			case "changed-definition":
				hash = "other"
			case "expired":
				at = now.Add(-25 * time.Hour)
			case "offline":
				i.Mode = "offline"
			case "cancelled":
				status = "cancelled"
				s.Status = status
			case "partial":
				status = "partial"
				s.Status = status
			case "zero-candidates":
				s.Candidates = 0
			case "field-errors":
				s.Output.Counts["invalid"] = 1
			case "key-conflict":
				s.Output.Counts["conflict"] = 1
			case "missing-output":
				s.Output = nil
			case "empty-rows":
				s.Output.Rows = nil
			case "unknown-decision":
				s.Output.Rows[0].Decision = "unknown"
			case "incompatible-schema":
				s.Output.Compatibility.Compatible = false
			}
			if CheckTrial(status, revision, hash, &at, i, s, 4, "hash", true, now).Ready {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
func TestPublishLimitedRequiresSpecificAcknowledgement(t *testing.T) {
	input, summary, now := trialFixture()
	summary.Status = "limited"
	summary.Reason = "BOUNDED_OR_OFFLINE_PATHS"
	summary.Warnings = []string{"LIST_PAGE_LIMIT_REACHED"}
	if CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", false, now).Ready {
		t.Fatal("unacknowledged limited trial passed")
	}
	if !CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", true, now).Ready {
		t.Fatal("bounded acknowledged trial failed")
	}
	summary.Warnings = []string{"OFFLINE_ACTION_NOT_EXECUTED"}
	if CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", true, now).Ready {
		t.Fatal("skipped path passed")
	}
	summary.Warnings = []string{"EXTRACTION_TRUNCATED"}
	summary.Output.Ready = false
	summary.Output.Warnings = []string{"DATABASE_STATE_MAY_CHANGE", "PREVIEW_INCOMPLETE"}
	if !CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", true, now).Ready {
		t.Fatal("bounded output cannot be acknowledged")
	}
	summary.Output.Compatibility.Compatible = false
	if CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", true, now).Ready {
		t.Fatal("schema incompatibility hidden by acknowledgement")
	}
	summary.Output.Compatibility.Compatible = true
	summary.Output.Counts["conflict"] = 1
	if CheckTrial("limited", 4, "hash", &now, input, summary, 4, "hash", true, now).Ready {
		t.Fatal("key conflict hidden by acknowledgement")
	}
}
func TestPublishCapabilitiesMatchActionsAndContracts(t *testing.T) {
	plan := trial.Plan{EntryType: "web", Steps: []trial.Step{{ID: "click", Kind: "action", Action: editor.Command{Type: "click"}}}}
	caps := Capabilities{Contract: ContractVersion, Interpreter: extraction.InterpreterVersion, BrowserProtocol: "editor.v1", Session: true, Commands: true, Snapshot: true, Actions: []string{"click"}}
	if !CheckCapabilities(plan, caps).Ready {
		t.Fatal("valid browser rejected")
	}
	caps.Actions = []string{"wait"}
	result := CheckCapabilities(plan, caps)
	if result.Ready || result.Issues[0].StepID != "click" {
		t.Fatal("unsupported action accepted", result)
	}
	caps.Actions = []string{"click"}
	caps.Snapshot = false
	if CheckCapabilities(plan, caps).Ready {
		t.Fatal("missing snapshot accepted")
	}
	plan.EntryType = "json"
	caps = Capabilities{Contract: ContractVersion, Interpreter: extraction.InterpreterVersion, HTTP: true, CredentialReady: true}
	if !CheckCapabilities(plan, caps).Ready {
		t.Fatal("JSON requires browser")
	}
	caps.CredentialReady = false
	if CheckCapabilities(plan, caps).Ready {
		t.Fatal("missing credential passed")
	}
}
func TestPublishSemanticHashPreservesLargeNumbers(t *testing.T) {
	a := `{"id":9007199254740993,"n":1e2}`
	b := `{"n":100,"id":9007199254740993}`
	c := `{"n":100,"id":9007199254740992}`
	if DefinitionHash(a) != DefinitionHash(b) || DefinitionHash(a) == DefinitionHash(c) {
		t.Fatal("JSONB normalization or integer precision broke publication hash")
	}
	if Hash(json.RawMessage(a)) != DefinitionHash(b) {
		t.Fatal("raw manifest hash differs")
	}
}

func TestContinuousDerivedActionCapabilities(t *testing.T) {
	p := trial.Plan{EntryType: "web", Steps: []trial.Step{{ID: "items", Kind: "record_set", Plan: extraction.Plan{HTML: editor.RecordPlan{NextPage: &editor.NextPageRule{Kind: "load_more", WaitMS: 500}}}}}}
	c := Capabilities{Contract: ContractVersion, Interpreter: extraction.InterpreterVersion, BrowserProtocol: "editor.v1", Session: true, Commands: true, Snapshot: true, Actions: []string{"navigate"}}
	if CheckCapabilities(p, c).Ready {
		t.Fatal("missing click/wait capability accepted")
	}
	c.Actions = editor.Actions
	if !CheckCapabilities(p, c).Ready {
		t.Fatal("supported derived path rejected")
	}
}
