// Package overview defines evidence-based summaries, without quality thresholds or inferred baselines.
package overview

import "time"

type Run struct {
	ID            string           `json:"run_id"`
	CollectorID   string           `json:"collector_id"`
	VersionNumber int              `json:"version_number"`
	Status        string           `json:"status"`
	Reason        string           `json:"stop_reason"`
	Committed     bool             `json:"committed"`
	Counts        map[string]int64 `json:"counts"`
	TableID       string           `json:"table_id"`
	CreatedAt     time.Time        `json:"created_at"`
	FinishedAt    *time.Time       `json:"finished_at"`
}
type Schedule struct {
	Enabled       bool       `json:"enabled"`
	NextAt        *time.Time `json:"next_at"`
	Timezone      string     `json:"timezone"`
	VersionID     string     `json:"version_id"`
	VersionNumber int        `json:"version_number"`
	LastDecision  string     `json:"last_decision"`
	DecisionAt    *time.Time `json:"decision_at"`
}
type Issue struct {
	ID       string `json:"issue_id"`
	Status   string `json:"status"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	RunID    string `json:"run_id"`
}
type Health struct {
	QualityIssues      []Issue   `json:"quality_issues"`
	AsOf               time.Time `json:"as_of"`
	CollectorID        string    `json:"collector_id"`
	Name               string    `json:"name"`
	EntryType          string    `json:"entry_type"`
	Archived           bool      `json:"archived"`
	PublishedVersionID *string   `json:"published_version_id"`
	PendingDraft       bool      `json:"pending_draft"`
	TableID            string    `json:"table_id"`
	TableName          string    `json:"table_name"`
	UpdatedAt          time.Time `json:"updated_at"`
	ActiveRuns         int       `json:"active_runs"`
	LatestRun          *Run      `json:"latest_run"`
	LatestTerminal     *Run      `json:"latest_terminal"`
	LatestEffective    *Run      `json:"latest_effective"`
	Schedule           *Schedule `json:"schedule"`
	Outcome            string    `json:"outcome"`
	Issues             []Issue   `json:"issues"`
	Baseline           string    `json:"baseline_status"`
}

// Evaluate never turns zero additions, intentional cancellation, overlap, or a
// newer in-flight run into an unsupported quality claim. Successful completion
// supersedes earlier failures; it does not silently resolve a disabled schedule.
func Evaluate(h *Health) {
	h.Issues = []Issue{}
	if h.Baseline == "" {
		h.Baseline = "not_configured"
	}
	h.Outcome = "never_run"
	if h.Archived {
		h.Outcome = "archived"
		return
	}
	if r := h.LatestTerminal; r != nil {
		switch r.Status {
		case "failed", "partial":
			h.Outcome = "needs_attention"
			h.Issues = append(h.Issues, Issue{Kind: "run_failed", Severity: "error", Code: r.Reason, RunID: r.ID})
		case "limited":
			h.Outcome = "limited"
			h.Issues = append(h.Issues, Issue{Kind: "limited_coverage", Severity: "warning", Code: r.Reason, RunID: r.ID})
		case "succeeded":
			h.Outcome = "healthy"
		case "cancelled":
			h.Outcome = "cancelled"
		default:
			h.Outcome = "unknown"
		}
	}
	if s := h.Schedule; s != nil {
		switch s.LastDecision {
		case "disabled_invalid", "disabled_unavailable", "skipped_capacity":
			h.Issues = append(h.Issues, Issue{Kind: "schedule_blocked", Severity: "warning", Code: s.LastDecision})
		}
	}
	if len(h.QualityIssues) > 0 {
		h.Issues = append(h.Issues, h.QualityIssues...)
		h.Outcome = "needs_attention"
	}
}

type Table struct {
	ID             string     `json:"table_id"`
	Name           string     `json:"name"`
	Records        string     `json:"record_count"`
	LastObservedAt *time.Time `json:"last_observed_at"`
	LastChangedAt  *time.Time `json:"last_changed_at"`
	LatestRunID    string     `json:"latest_run_id"`
}
type Home struct {
	Status           string            `json:"status"`
	AsOf             time.Time         `json:"as_of"`
	WindowStart      time.Time         `json:"window_start"`
	Baseline         string            `json:"baseline_status"`
	Counts           map[string]string `json:"counts"`
	Activity         map[string]string `json:"activity"`
	Attention        []Health          `json:"attention"`
	RecentCollectors []Health          `json:"recent_collectors"`
	PendingDrafts    []Health          `json:"pending_drafts"`
	NextSchedules    []Health          `json:"next_schedules"`
	RecentTables     []Table           `json:"recent_tables"`
	RecentRuns       []Run             `json:"recent_runs"`
	Limit            int               `json:"section_limit"`
}
