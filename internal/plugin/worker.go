package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/repo/plugin_repo"
	log "github.com/sirupsen/logrus"
)

const (
	LeaseDuration        = 5 * time.Minute
	PollInterval         = 500 * time.Millisecond
	ExternalPollInterval = 10 * time.Second
)

type Worker struct {
	registry     *Registry
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	count        int
	mu           sync.RWMutex
	running      bool
	active       int
	startedAt    *time.Time
	stoppedAt    *time.Time
	lastActivity *time.Time
}

type WorkerSnapshot struct {
	Running      bool       `json:"running"`
	WorkerCount  int        `json:"worker_count"`
	Active       int        `json:"active"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	StoppedAt    *time.Time `json:"stopped_at,omitempty"`
	LastActivity *time.Time `json:"last_activity,omitempty"`
}

func NewWorker(registry *Registry) *Worker { return &Worker{registry: registry} }
func (w *Worker) Name() string             { return "PluginWorker" }

func (w *Worker) Start(parent context.Context) error {
	if w.registry == nil {
		return errors.New("plugin registry is required")
	}
	cfg := bean.PtrFromContext[config.Config](parent)
	count := 1
	if cfg != nil && cfg.Crawler != nil && cfg.Crawler.WorkerNum > 0 {
		count = cfg.Crawler.WorkerNum
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	now := time.Now()
	w.mu.Lock()
	w.running, w.active, w.count, w.startedAt, w.stoppedAt = true, 0, count, &now, nil
	w.mu.Unlock()
	for i := 0; i < count; i++ {
		w.wg.Add(1)
		go w.loop(ctx, i)
	}
	return nil
}

func (w *Worker) Stop(_ context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	now := time.Now()
	w.mu.Lock()
	w.running, w.active, w.stoppedAt = false, 0, &now
	w.mu.Unlock()
	return nil
}

func (w *Worker) Snapshot() WorkerSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return WorkerSnapshot{
		Running: w.running, WorkerCount: w.count, Active: w.active,
		StartedAt: w.startedAt, StoppedAt: w.stoppedAt, LastActivity: w.lastActivity,
	}
}

func (w *Worker) taskStarted() {
	now := time.Now()
	w.mu.Lock()
	w.active++
	w.lastActivity = &now
	w.mu.Unlock()
}

func (w *Worker) taskFinished() {
	now := time.Now()
	w.mu.Lock()
	if w.active > 0 {
		w.active--
	}
	w.lastActivity = &now
	w.mu.Unlock()
}

func (w *Worker) loop(ctx context.Context, index int) {
	defer w.wg.Done()
	workerID := fmt.Sprintf("plugin-%d-%s", index, uuid.NewString()[:8])
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claim, found, err := plugin_repo.ClaimNext(workerID, LeaseDuration)
		if err != nil && !pluginRepoUnavailable(err) {
			log.Warnf("领取插件任务失败: %s", err)
		}
		if err != nil || !found {
			if !wait(ctx, PollInterval) {
				return
			}
			continue
		}
		w.processSafely(ctx, claim)
	}
}

func (w *Worker) processSafely(ctx context.Context, claim *plugin_repo.Claim) {
	w.taskStarted()
	defer w.taskFinished()
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("plugin task panic: %v", recovered)
			log.Errorf("插件任务执行 panic: task=%d error=%s", claim.Task.Id, err)
			_ = plugin_repo.Fail(claim.Task.Id, err, false)
		}
	}()
	w.processClaim(ctx, claim)
}

func (w *Worker) processClaim(ctx context.Context, claim *plugin_repo.Claim) {
	handler, ok := w.registry.Get(claim.Task.PluginCode)
	if !ok {
		_ = plugin_repo.Fail(claim.Task.Id, fmt.Errorf("plugin %q is not registered", claim.Task.PluginCode), false)
		return
	}
	input := map[string]any{}
	if err := json.Unmarshal([]byte(claim.Task.Input), &input); err != nil {
		_ = plugin_repo.Fail(claim.Task.Id, err, false)
		return
	}
	task := Task{ID: claim.Task.Id, IdempotencyKey: claim.Task.IdempotencyKey, ResourceID: claim.Task.ResourceId, EventType: claim.Task.EventType, Input: input}
	if claim.Task.RecordId != nil {
		task.RecordID = *claim.Task.RecordId
		optIn, supported := handler.(RecordHandler)
		if !supported || !optIn.SupportsRecordEvents() {
			_ = plugin_repo.Fail(claim.Task.Id, errors.New("plugin does not support record events"), false)
			return
		}
	}
	async, isAsync := handler.(AsyncHandler)
	if isAsync && claim.Task.ExternalID != "" {
		output, done, err := async.Poll(ctx, task, claim.Task.ExternalID)
		if err != nil {
			_ = plugin_repo.Fail(claim.Task.Id, err, !isPermanent(err))
			return
		}
		if !done {
			if err := plugin_repo.SchedulePoll(claim.Task.Id, output, claim.Task.ExternalID, ExternalPollInterval); err != nil {
				log.Errorf("重新调度插件轮询失败: %s", err)
			}
			return
		}
		if completion, ok := handler.(CompletionHandler); ok {
			if err := completion.OnComplete(ctx, task, output); err != nil {
				_ = plugin_repo.Fail(claim.Task.Id, err, true)
				return
			}
		}
		if err := plugin_repo.Complete(claim.Task.Id, output, claim.Task.ExternalID); err != nil {
			log.Errorf("完成插件任务失败: %s", err)
		}
		return
	}

	output, externalID, err := handler.Handle(ctx, task)
	if err != nil {
		_ = plugin_repo.Fail(claim.Task.Id, err, !isPermanent(err))
		return
	}
	if isAsync && externalID != "" {
		if err := plugin_repo.SchedulePoll(claim.Task.Id, output, externalID, ExternalPollInterval); err != nil {
			log.Errorf("调度插件轮询失败: %s", err)
		}
		return
	}
	if completion, ok := handler.(CompletionHandler); ok {
		if err := completion.OnComplete(ctx, task, output); err != nil {
			_ = plugin_repo.Fail(claim.Task.Id, err, true)
			return
		}
	}
	if err := plugin_repo.Complete(claim.Task.Id, output, externalID); err != nil {
		log.Errorf("完成插件任务失败: %s", err)
	}
}

func isPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func pluginRepoUnavailable(err error) bool {
	return err != nil && err.Error() == "database is not initialized"
}
