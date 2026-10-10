// Package quality evaluates formal evidence, never offline regression or new-row counts.
package quality

import (
	"encoding/json"
	"errors"
	"github.com/nekoimi/scrapio/internal/publication"
	"sort"
	"strings"
	"time"
)

type Policy struct {
	Enabled              bool `json:"enabled"`
	MinValid             int  `json:"min_valid_records"`
	MaxInvalidPercent    int  `json:"max_invalid_percent"`
	DropPercent          int  `json:"drop_percent"`
	FailureRuns          int  `json:"failure_runs"`
	RecoveryRuns         int  `json:"recovery_runs"`
	ScheduleGraceMinutes int  `json:"schedule_grace_minutes"`
}

func Default() Policy {
	return Policy{Enabled: true, MaxInvalidPercent: 20, DropPercent: 50, FailureRuns: 2, RecoveryRuns: 2, ScheduleGraceMinutes: 10}
}
func (p Policy) Validate() error {
	if p.MinValid < 0 || p.MinValid > 1000 || p.MaxInvalidPercent < 0 || p.MaxInvalidPercent > 100 || p.DropPercent < 0 || p.DropPercent > 100 || p.FailureRuns < 1 || p.FailureRuns > 10 || p.RecoveryRuns < 1 || p.RecoveryRuns > 10 || p.ScheduleGraceMinutes < 1 || p.ScheduleGraceMinutes > 1440 {
		return errors.New("quality thresholds outside bounds")
	}
	return nil
}

type Facts struct {
	TriggerSource string    `json:"trigger_source"`
	RunID         string    `json:"run_id"`
	VersionID     string    `json:"version_id"`
	Key           string    `json:"comparison_key"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason"`
	Committed     bool      `json:"committed"`
	Network       bool      `json:"network_accessed"`
	Valid         int       `json:"valid_records"`
	Invalid       int       `json:"invalid_records"`
	FieldKeys     []string  `json:"field_keys"`
	Pages         int       `json:"pages"`
	ListPages     int       `json:"list_pages"`
	FailedStep    string    `json:"failed_step_id"`
	FailedStage   string    `json:"failed_stage"`
	FinishedAt    time.Time `json:"finished_at"`
	StartedAt     time.Time `json:"started_at"`
}

func (f Facts) Complete() bool {
	return f.Status == "succeeded" && f.Committed && f.Network && f.Pages > 0 && f.FailedStep == "" && f.FailedStage == "" && f.Key != ""
}

// Comparable preserves scope/coverage/output identity while allowing selector fixes.
func Comparable(definition, schema, input, entryType string) string {
	var root map[string]json.RawMessage
	var i struct {
		Origins []string        `json:"allowed_origins"`
		Budget  json.RawMessage `json:"budget"`
	}
	if json.Unmarshal([]byte(definition), &root) != nil || json.Unmarshal([]byte(input), &i) != nil || !json.Valid([]byte(schema)) {
		return ""
	}
	var steps []map[string]json.RawMessage
	if json.Unmarshal(root["steps"], &steps) != nil {
		return ""
	}
	paths := []any{}
	for _, step := range steps {
		var kind string
		_ = json.Unmarshal(step["type"], &kind)
		if kind != "record_set" && kind != "json_records" {
			paths = append(paths, step)
			continue
		}
		var config map[string]json.RawMessage
		if json.Unmarshal(step["config"], &config) != nil {
			return ""
		}
		delete(config, "fields")
		delete(config, "detail_fields")
		delete(config, "locator")
		paths = append(paths, map[string]any{"step_id": step["step_id"], "type": kind, "config": config})
	}
	var output map[string]json.RawMessage
	if json.Unmarshal(root["output"], &output) != nil {
		return ""
	}
	delete(output, "check_id")
	sort.Strings(i.Origins)
	return publication.Hash([]any{entryType, root["entry_url"], root["http_request"], root["browser_auth"], paths, output, json.RawMessage(schema), i})
}

type Finding struct {
	Code     string `json:"code"`
	Category string `json:"category"`
	Message  string `json:"message"`
}
type Result struct {
	Findings         []Finding `json:"findings"`
	BaselineStatus   string    `json:"baseline_status"`
	RecoveryEligible bool      `json:"recovery_eligible"`
	FailureStreak    int       `json:"failure_streak"`
}

func Evaluate(p Policy, f Facts, base *Facts, previousFailures int) Result {
	r := Result{Findings: []Finding{}, BaselineStatus: "not_configured"}
	if base != nil {
		r.BaselineStatus = "incomparable"
		if base.Complete() && base.Key == f.Key && base.FinishedAt.Before(f.FinishedAt) {
			r.BaselineStatus = "comparable"
		}
	}
	if !p.Enabled {
		return r
	}
	if f.Status == "cancelled" {
		r.FailureStreak = previousFailures
		return r
	}
	add := func(code, category, msg string) { r.Findings = append(r.Findings, Finding{code, category, msg}) }
	if f.Status == "failed" || f.Status == "partial" {
		r.FailureStreak = previousFailures + 1
		reason := strings.ToUpper(f.Reason)
		if strings.Contains(reason, "BROWSER") || strings.Contains(reason, "SESSION") || strings.Contains(reason, "RPC") {
			add("BROWSER_FAILURE", "browser", "浏览器执行或会话故障，请查看实际运行证据")
		} else if len(f.FieldKeys) > 0 {
			add("FIELD_MISMATCH", "extraction", "提取字段存在校验错误，请定位步骤和字段")
		}
		if r.FailureStreak >= p.FailureRuns {
			add("CONSECUTIVE_FAILURES", "execution", "正式运行连续失败达到策略阈值")
		}
	}
	// Incomplete coverage cannot be used for volume comparisons or recovery.
	if f.Status == "limited" {
		add("LIMITED_COVERAGE", "coverage", "运行有限覆盖，不能证明完整恢复")
	}
	if f.Complete() || (f.Status == "partial" && f.Network && len(f.FieldKeys) > 0) {
		if p.MinValid > 0 && f.Valid < p.MinValid {
			add("VALID_RECORDS_LOW", "volume", "有效提取记录量低于明确配置的最低数量")
		}
		if f.Valid+f.Invalid > 0 && f.Invalid*100 > (f.Valid+f.Invalid)*p.MaxInvalidPercent {
			add("INVALID_RATIO_HIGH", "extraction", "无效提取记录比例超过策略阈值")
		}
	}
	if f.Complete() && p.DropPercent > 0 && r.BaselineStatus == "comparable" && base.Valid > 0 && f.Valid*100 < base.Valid*(100-p.DropPercent) {
		add("VALID_RECORDS_DROP", "volume", "有效记录量相对同范围基线下降超过阈值")
	}
	r.RecoveryEligible = f.Complete() && len(r.Findings) == 0 && (base == nil || r.BaselineStatus == "comparable")
	return r
}
