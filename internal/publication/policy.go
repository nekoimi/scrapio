// Package publication validates immutable v2.2 publication evidence without I/O.
package publication

import (
	"encoding/json"
	"time"

	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/sample"
	"github.com/nekoimi/scrapio/internal/trial"
)

const ContractVersion = "publish.v1"

type Capabilities struct {
	DefinitionHash  string   `json:"definition_hash"`
	Contract        string   `json:"contract"`
	Interpreter     string   `json:"interpreter"`
	BrowserProtocol string   `json:"browser_protocol"`
	Session         bool     `json:"session"`
	Commands        bool     `json:"commands"`
	Snapshot        bool     `json:"snapshot"`
	HTTP            bool     `json:"http"`
	CredentialReady bool     `json:"credential_ready"`
	Actions         []string `json:"actions"`
}
type Issue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	StepID   string `json:"step_id,omitempty"`
	FieldKey string `json:"field_key,omitempty"`
	SampleID string `json:"sample_id,omitempty"`
	Stage    string `json:"stage,omitempty"`
}
type Result struct {
	Ready    bool    `json:"ready"`
	Issues   []Issue `json:"issues"`
	Warnings []Issue `json:"warnings"`
}

func NewResult() Result { return Result{Ready: true, Issues: []Issue{}, Warnings: []Issue{}} }
func (r *Result) Block(code, message, step, stage, sampleID, fieldKey string) {
	r.Ready = false
	r.Issues = append(r.Issues, Issue{code, message, step, fieldKey, sampleID, stage})
}
func (r *Result) Warn(code, message string) {
	r.Warnings = append(r.Warnings, Issue{Code: code, Message: message})
}
func Hash(v any) string                { raw, _ := json.Marshal(v); return capture.Hash(sample.CanonicalJSON(raw)) }
func DefinitionHash(raw string) string { return capture.Hash(sample.CanonicalJSON([]byte(raw))) }
func CheckCapabilities(plan trial.Plan, c Capabilities) Result {
	r := NewResult()
	if c.Contract != ContractVersion || c.Interpreter != extraction.InterpreterVersion {
		r.Block("CONTRACT_UNAVAILABLE", "当前发布/提取契约不可用", "", "", "", "")
	}
	if plan.EntryType == "web" {
		if c.BrowserProtocol != "editor.v1" || !c.Session || !c.Commands || !c.Snapshot {
			r.Block("BROWSER_CAPABILITY_UNAVAILABLE", "浏览器会话、动作及快照能力必须可用", "", "", "", "")
		}
		supported := map[string]bool{}
		for _, action := range c.Actions {
			supported[action] = true
		}
		for _, step := range plan.Steps {
			if step.Kind == "action" || step.Kind == "navigate" {
				if !supported[step.Action.Type] {
					r.Block("ACTION_UNSUPPORTED", "动作不受当前浏览器协议支持", step.ID, "action", "", "")
				}
			}
		}
	} else if !c.HTTP || !c.CredentialReady {
		r.Block("HTTP_CAPABILITY_UNAVAILABLE", "HTTP 执行或引用凭据不可用", "", "", "", "")
	}
	return r
}

// Limited trials can only be acknowledged for known finite coverage warnings.
// Skipped offline paths, cancellation, failed stages and missing output never pass.
func CheckTrial(rowStatus string, revision int, hash string, finished *time.Time, input trial.Input, summary trial.Summary, expectedRevision int, definitionHash string, acceptLimited bool, now time.Time) Result {
	r := NewResult()
	if revision != expectedRevision || hash != definitionHash {
		r.Block("TRIAL_STALE", "试采对应的草稿已变化，请重新试采", "", "", "", "")
	}
	if finished == nil || finished.After(now) || now.Sub(*finished) > 24*time.Hour {
		r.Block("TRIAL_EXPIRED", "试采证据需在最近 24 小时内完成", "", "", "", "")
	}
	if input.Mode != "live" || !input.Confirmed {
		r.Block("LIVE_TRIAL_REQUIRED", "发布前需从入口执行已确认的实时试采；离线只能验证字段", "", "", "", "")
	}
	if input.Validate() != nil {
		r.Block("TRIAL_CONFIG_INVALID", "试采范围或预算无效", "", "", "", "")
	}
	if rowStatus != summary.Status || rowStatus != "succeeded" && rowStatus != "limited" || summary.FailedStep != "" || summary.FailedStage != "" {
		r.Block("TRIAL_NOT_PASSED", "试采失败、部分通过或提前停止，不能发布", summary.FailedStep, summary.FailedStage, "", "")
	}
	outputUsable := summary.Output != nil && summary.Output.Ready
	if summary.Output != nil && !summary.Output.Ready && rowStatus == "limited" && acceptLimited && summary.Output.Compatibility.Compatible && len(summary.Output.Compatibility.Issues) == 0 && len(summary.Output.Rows) > 0 {
		// Truncation alone can be explicitly acknowledged, never type/key/field
		// errors, empty candidates or arbitrary unknown warnings.
		truncated := false
		onlyBounded := true
		for _, warning := range summary.Output.Warnings {
			if warning == "PREVIEW_INCOMPLETE" {
				truncated = true
			} else if warning != "DATABASE_STATE_MAY_CHANGE" {
				onlyBounded = false
			}
		}
		outputUsable = truncated && onlyBounded
	}
	if summary.Output != nil {
		outputUsable = outputUsable && summary.Output.Compatibility.Compatible && len(summary.Output.Compatibility.Issues) == 0 && len(summary.Output.Rows) > 0
		for _, row := range summary.Output.Rows {
			if len(row.Issues) > 0 || row.Decision != "created" && row.Decision != "updated" && row.Decision != "unchanged" {
				outputUsable = false
			}
		}
	}
	if summary.Output == nil || !outputUsable || summary.Candidates < 1 || summary.Output.Counts["invalid"] > 0 || summary.Output.Counts["conflict"] > 0 {
		r.Block("TRIAL_OUTPUT_BLOCKED", "试采需有有效候选并完成无阻断的输出预判", "", "", "", "")
	}
	if rowStatus == "limited" {
		allowed := map[string]bool{"EXTRACTION_TRUNCATED": true, "DETAIL_BUDGET_REACHED": true, "RECORD_BUDGET_REACHED": true, "LIST_PAGE_LIMIT_REACHED": true}
		known := len(summary.Warnings) > 0 && summary.Reason == "BOUNDED_OR_OFFLINE_PATHS"
		for _, warning := range summary.Warnings {
			if !allowed[warning] {
				known = false
			}
		}
		if !known {
			r.Block("TRIAL_INCOMPLETE", "存在未执行/失败路径，不能确认有限覆盖后发布", "", "", "", "")
		} else if !acceptLimited {
			r.Block("LIMITED_COVERAGE_CONFIRMATION_REQUIRED", "试采覆盖有限，请查看限制并显式确认", "", "", "", "")
		} else {
			r.Warn("LIMITED_COVERAGE_ACCEPTED", "已确认试采有限覆盖，未覆盖部分仍有运行风险")
		}
	}
	return r
}
func Merge(a *Result, b Result) {
	a.Ready = a.Ready && b.Ready
	a.Issues = append(a.Issues, b.Issues...)
	a.Warnings = append(a.Warnings, b.Warnings...)
}
