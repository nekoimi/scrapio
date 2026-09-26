package plugin_repo

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nekoimi/scrapio/internal/db"
	"github.com/nekoimi/scrapio/internal/db/table"
	"xorm.io/xorm"
)

const (
	taskQueued    = "queued"
	taskRunning   = "running"
	taskSucceeded = "succeeded"
	taskFailed    = "failed"
	taskCancelled = "cancelled"
)

type EnqueueInput struct {
	ResourceID     int64
	EventType      string
	PluginCode     string
	IdempotencyKey string
	Input          any
	MaxAttempts    int
}

type Claim struct {
	Task table.PluginTask
}

type ListFilter struct {
	ResourceID int64
	DatasetID  int64
	WorkflowID int64
	PluginCode string
	EventType  string
	Status     string
	Page       int
	Size       int
}

type PluginCount struct {
	PluginCode string `xorm:"plugin_code" json:"plugin_code"`
	Total      int64  `xorm:"total" json:"total"`
	Queued     int64  `xorm:"queued" json:"queued"`
	Running    int64  `xorm:"running" json:"running"`
	Succeeded  int64  `xorm:"succeeded" json:"succeeded"`
	Failed     int64  `xorm:"failed" json:"failed"`
	Cancelled  int64  `xorm:"cancelled" json:"cancelled"`
}

type MetricsSnapshot struct {
	Total        int64            `json:"total"`
	StatusCounts map[string]int64 `json:"status_counts"`
	PluginCounts []PluginCount    `json:"plugin_counts"`
	GeneratedAt  time.Time        `json:"generated_at"`
}

func Enqueue(input EnqueueInput) (*table.PluginTask, bool, error) {
	if db.Instance() == nil {
		return nil, false, errors.New("database is not initialized")
	}
	if input.ResourceID <= 0 || strings.TrimSpace(input.EventType) == "" || strings.TrimSpace(input.PluginCode) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, false, errors.New("resource, event, plugin and idempotency key are required")
	}
	encoded, err := json.Marshal(input.Input)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) > 1024*1024 {
		return nil, false, errors.New("plugin input exceeds size limit")
	}
	max := input.MaxAttempts
	if max <= 0 {
		max = 5
	}
	row := &table.PluginTask{ResourceId: input.ResourceID, EventType: input.EventType, PluginCode: input.PluginCode, IdempotencyKey: input.IdempotencyKey, Status: taskQueued, MaxAttempts: max, Input: string(encoded), Output: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	inserted, err := db.Instance().InsertOne(row)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			existing := new(table.PluginTask)
			has, getErr := db.Instance().Where("plugin_code = ? AND idempotency_key = ?", input.PluginCode, input.IdempotencyKey).Get(existing)
			if getErr != nil {
				return nil, false, getErr
			}
			if has {
				return existing, false, nil
			}
		}
		return nil, false, err
	}
	return row, inserted > 0, nil
}

func Get(id int64) (*table.PluginTask, bool, error) {
	if db.Instance() == nil {
		return nil, false, errors.New("database is not initialized")
	}
	row := new(table.PluginTask)
	has, err := db.Instance().ID(id).Get(row)
	return row, has, err
}

func List(resourceID int64, status string, page, size int) ([]table.PluginTask, int64, error) {
	return ListFiltered(ListFilter{ResourceID: resourceID, Status: status, Page: page, Size: size})
}

func ListFiltered(filter ListFilter) ([]table.PluginTask, int64, error) {
	if db.Instance() == nil {
		return nil, 0, errors.New("database is not initialized")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Size <= 0 {
		filter.Size = 20
	}
	if filter.Size > 200 {
		filter.Size = 200
	}
	count := db.Instance().NewSession()
	defer count.Close()
	applyListFilter(count, filter)
	total, err := count.Count(new(table.PluginTask))
	if err != nil {
		return nil, 0, err
	}
	s := db.Instance().NewSession()
	defer s.Close()
	applyListFilter(s, filter)
	rows := make([]table.PluginTask, 0)
	err = s.Desc("created_at").Limit(filter.Size, (filter.Page-1)*filter.Size).Find(&rows)
	return rows, total, err
}

func applyListFilter(s *xorm.Session, filter ListFilter) {
	s.Where("1 = 1")
	if filter.ResourceID > 0 {
		s.And("resource_id = ?", filter.ResourceID)
	}
	if filter.DatasetID > 0 {
		s.And("dataset_id = ?", filter.DatasetID)
	}
	if filter.WorkflowID > 0 {
		s.And("workflow_id = ?", filter.WorkflowID)
	}
	if strings.TrimSpace(filter.PluginCode) != "" {
		s.And("plugin_code = ?", strings.TrimSpace(filter.PluginCode))
	}
	if strings.TrimSpace(filter.EventType) != "" {
		s.And("event_type = ?", strings.TrimSpace(filter.EventType))
	}
	if strings.TrimSpace(filter.Status) != "" {
		s.And("status = ?", strings.TrimSpace(filter.Status))
	}
}

func Metrics() (MetricsSnapshot, error) {
	result := MetricsSnapshot{StatusCounts: map[string]int64{}, PluginCounts: []PluginCount{}, GeneratedAt: time.Now()}
	if db.Instance() == nil {
		return result, errors.New("database is not initialized")
	}
	type statusCount struct {
		Status string `xorm:"status"`
		Total  int64  `xorm:"total"`
	}
	var statuses []statusCount
	if err := db.Instance().Table(new(table.PluginTask)).Select("status, COUNT(*) AS total").GroupBy("status").Find(&statuses); err != nil {
		return result, err
	}
	for _, row := range statuses {
		result.StatusCounts[row.Status] = row.Total
		result.Total += row.Total
	}
	err := db.Instance().Table(new(table.PluginTask)).Select(`plugin_code, COUNT(*) AS total,
		COUNT(*) FILTER (WHERE status = 'queued') AS queued,
		COUNT(*) FILTER (WHERE status = 'running') AS running,
		COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
		COUNT(*) FILTER (WHERE status = 'failed') AS failed,
		COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled`).
		GroupBy("plugin_code").OrderBy("plugin_code ASC").Find(&result.PluginCounts)
	return result, err
}

func MarkRunning(id int64) error {
	if db.Instance() == nil {
		return errors.New("database is not initialized")
	}
	_, err := db.Instance().ID(id).Cols("status", "attempt_count", "updated_at").Where("status = ?", taskQueued).Update(&table.PluginTask{Status: taskRunning, AttemptCount: 1, UpdatedAt: time.Now()})
	return err
}

// ClaimNext atomically leases one plugin task. Delivery plugins use this
// durable queue instead of the legacy downloader scheduler.
func ClaimNext(workerID string, lease time.Duration) (*Claim, bool, error) {
	if db.Instance() == nil {
		return nil, false, errors.New("database is not initialized")
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	s := db.Instance().NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return nil, false, err
	}
	var task table.PluginTask
	condition := "((status = ? AND (next_retry_at IS NULL OR next_retry_at <= NOW())) OR (status = ? AND lease_until < NOW()))"
	has, err := s.Where(condition, taskQueued, taskRunning).Asc("created_at").Limit(1).Get(&task)
	if err != nil || !has {
		_ = s.Rollback()
		return nil, false, err
	}
	now := time.Now()
	until := now.Add(lease)
	attemptCount := task.AttemptCount
	if strings.TrimSpace(task.ExternalID) == "" {
		attemptCount++
	}
	result, err := s.ID(task.Id).Cols("status", "attempt_count", "lease_owner", "lease_until", "updated_at").Where(condition, taskQueued, taskRunning).Update(&table.PluginTask{Status: taskRunning, AttemptCount: attemptCount, LeaseOwner: workerID, LeaseUntil: &until, UpdatedAt: now})
	if err != nil || result == 0 {
		_ = s.Rollback()
		return nil, false, err
	}
	if err := s.Commit(); err != nil {
		return nil, false, err
	}
	task.Status, task.AttemptCount, task.LeaseOwner, task.LeaseUntil = taskRunning, attemptCount, workerID, &until
	return &Claim{Task: task}, true, nil
}

func Cancel(id int64) error {
	if db.Instance() == nil {
		return errors.New("database is not initialized")
	}
	now := time.Now()
	affected, err := db.Instance().ID(id).Cols("status", "next_retry_at", "lease_owner", "lease_until", "updated_at", "finished_at").Where("status IN (?, ?)", taskQueued, taskRunning).Update(&table.PluginTask{Status: taskCancelled, NextRetryAt: nil, LeaseOwner: "", LeaseUntil: nil, UpdatedAt: now, FinishedAt: &now})
	if err == nil && affected == 0 {
		return errors.New("only queued or running plugin tasks can be cancelled")
	}
	return err
}

func Retry(id int64) error {
	if db.Instance() == nil {
		return errors.New("database is not initialized")
	}
	affected, err := db.Instance().ID(id).Cols("status", "attempt_count", "next_retry_at", "lease_owner", "lease_until", "external_id", "output", "error_message", "finished_at", "updated_at").Where("status IN (?, ?)", taskFailed, taskCancelled).Update(&table.PluginTask{Status: taskQueued, AttemptCount: 0, NextRetryAt: nil, LeaseOwner: "", LeaseUntil: nil, ExternalID: "", Output: "{}", ErrorMessage: "", FinishedAt: nil, UpdatedAt: time.Now()})
	if err == nil && affected == 0 {
		return errors.New("only failed or cancelled plugin tasks can be retried")
	}
	return err
}

func Complete(id int64, output any, externalID string) error {
	return finish(id, taskSucceeded, output, externalID, "")
}

// SchedulePoll puts an asynchronous external task back in the durable queue.
// The task remains unfinished and will be leased again after delay.
func SchedulePoll(id int64, output any, externalID string, delay time.Duration) error {
	if db.Instance() == nil {
		return errors.New("database is not initialized")
	}
	if strings.TrimSpace(externalID) == "" {
		return errors.New("external id is required for polling")
	}
	encoded := "{}"
	if output != nil {
		data, err := json.Marshal(output)
		if err != nil {
			return err
		}
		if len(data) > 1024*1024 {
			return errors.New("plugin output exceeds size limit")
		}
		encoded = string(data)
	}
	if delay < 0 {
		delay = 0
	}
	next := time.Now().Add(delay)
	_, err := db.Instance().ID(id).Where("status = ?", taskRunning).Cols("status", "output", "external_id", "next_retry_at", "lease_owner", "lease_until", "error_message", "updated_at", "finished_at").Update(&table.PluginTask{
		Status: taskQueued, Output: encoded, ExternalID: externalID, NextRetryAt: &next,
		LeaseOwner: "", LeaseUntil: nil, ErrorMessage: "", UpdatedAt: time.Now(), FinishedAt: nil,
	})
	return err
}

func Fail(id int64, cause error, retryable bool) error {
	message := "plugin task failed"
	if cause != nil {
		message = cause.Error()
	}
	row, has, err := Get(id)
	if err != nil || !has {
		return err
	}
	attemptCount := row.AttemptCount
	if strings.TrimSpace(row.ExternalID) != "" {
		attemptCount++
	}
	if retryable && attemptCount < row.MaxAttempts {
		next := time.Now().Add(time.Duration(1<<min(attemptCount, 6)) * time.Second)
		_, err = db.Instance().ID(id).Where("status = ?", taskRunning).Cols("status", "attempt_count", "next_retry_at", "lease_owner", "lease_until", "error_message", "updated_at").Update(&table.PluginTask{Status: taskQueued, AttemptCount: attemptCount, NextRetryAt: &next, LeaseOwner: "", LeaseUntil: nil, ErrorMessage: message, UpdatedAt: time.Now()})
		return err
	}
	var output any
	if json.Valid([]byte(row.Output)) {
		output = json.RawMessage(row.Output)
	}
	if attemptCount != row.AttemptCount {
		if _, err := db.Instance().ID(id).Where("status = ?", taskRunning).Cols("attempt_count").Update(&table.PluginTask{AttemptCount: attemptCount}); err != nil {
			return err
		}
	}
	return finish(id, taskFailed, output, row.ExternalID, message)
}

func finish(id int64, status string, output any, externalID, message string) error {
	encoded := "{}"
	if output != nil {
		data, err := json.Marshal(output)
		if err != nil {
			return err
		}
		if len(data) > 1024*1024 {
			return errors.New("plugin output exceeds size limit")
		}
		encoded = string(data)
	}
	now := time.Now()
	_, err := db.Instance().ID(id).Where("status = ?", taskRunning).Cols("status", "output", "external_id", "error_message", "lease_owner", "lease_until", "updated_at", "finished_at").Update(&table.PluginTask{Status: status, Output: encoded, ExternalID: externalID, ErrorMessage: message, LeaseOwner: "", LeaseUntil: nil, UpdatedAt: now, FinishedAt: &now})
	return err
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
