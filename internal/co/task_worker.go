package co

import (
	"context"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	log "github.com/sirupsen/logrus"
	"os"
	"strconv"
	"sync"
	"time"
	xormlog "xorm.io/xorm/log"
)

// TaskWorker has its own concrete lifecycle key, distinct from O1 operations.
type TaskWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewTaskWorker() *TaskWorker { return &TaskWorker{} }
func (*TaskWorker) Name() string { return "coTaskWorker" }
func slots(name string, fallback int) int {
	n, e := strconv.Atoi(os.Getenv(name))
	if e != nil || n < 1 || n > 8 {
		return fallback
	}
	return n
}
func (w *TaskWorker) Start(ctx context.Context) error {
	run, cancel := context.WithCancel(context.Background())
	run = context.WithValue(run, xormlog.SessionShowSQLKey, false)
	w.cancel = cancel
	w.done = make(chan struct{})
	browser := bean.PtrFromContext[drission_rod.DrissionRod](ctx)
	cfg := bean.PtrFromContext[config.Config](ctx)
	go func() {
		defer close(w.done)
		var group sync.WaitGroup
		group.Add(1)
		go func() {
			defer group.Done()
			ticker := time.NewTicker(time.Second)
			nextCleanup := time.Now().Add(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-run.Done():
					return
				case <-ticker.C:
					if time.Now().After(nextCleanup) {
						if e := CleanupSources(run); e != nil && run.Err() == nil {
							log.WithError(e).Warn("retain independent source evidence")
						}
						nextCleanup = time.Now().Add(time.Hour)
						if e := CleanupChanges(run); e != nil && run.Err() == nil {
							log.WithError(e).Warn("retain independent table changes")
						}
					}
					if e := TickSchedules(run); e != nil && run.Err() == nil {
						log.WithError(e).Warn("schedule independent tasks")
					}
					if e := TickSubscriptions(run); e != nil && run.Err() == nil {
						log.WithError(e).Warn("dispatch independent task subscriptions")
					}
					if e := PrepareRuns(run); e != nil && run.Err() == nil {
						log.WithError(e).Warn("prepare independent task inputs")
					}
					if e := DispatchRuns(run); e != nil && run.Err() == nil {
						log.WithError(e).Warn("dispatch independent task outputs")
					}
				}
			}
		}()
		for _, kind := range []bool{false, true} {
			count := slots("SCRAPIO_CO_HTTP_SLOTS", 2)
			if kind {
				count = slots("SCRAPIO_CO_BROWSER_SLOTS", 1)
			}
			for i := 0; i < count; i++ {
				group.Add(1)
				go func(browserOnly bool) {
					defer group.Done()
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-run.Done():
							return
						case <-ticker.C:
							c, e := ClaimWork(run, browserOnly)
							if e != nil {
								if run.Err() == nil {
									log.WithError(e).Warn("claim independent task item")
								}
								continue
							}
							if c != nil {
								w.execute(run, c, browser, cfg)
							}
						}
					}
				}(kind)
			}
		}
		group.Wait()
	}()
	return nil
}
func (w *TaskWorker) Stop(ctx context.Context) error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *TaskWorker) execute(parent context.Context, c *WorkClaim, browser *drission_rod.DrissionRod, cfg *config.Config) {
	if c.Recovery {
		w.recoverCommands(parent, c, browser)
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := Heartbeat(ctx, c); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	var result collector.Result
	func() {
		defer func() {
			if recover() != nil {
				result = collector.Result{Status: "failed", Reason: "WORKER_PANIC", Error: collector.AsError(collector.Fault("WORKER_PANIC", "worker", "执行异常，已保存批次保留"))}
			}
		}()
		var input map[string]any
		var cp collector.Checkpoint
		if e := collector.Strict([]byte(c.Item.InputValues), &input); e != nil {
			result = collector.Result{Status: "failed", Error: collector.AsError(e)}
			return
		}
		if e := collector.Strict([]byte(c.Item.Checkpoint), &cp); e != nil {
			result = collector.Result{Status: "failed", Error: collector.AsError(e)}
			return
		}
		d := c.Manifest.CollectorDefinition
		source := &Source{Browser: browser, Config: cfg, Owner: c.Run.OwnerID, Definition: d, ID: c.Attempt.SessionID, Mode: "live", Journal: func(id string) error { return JournalCommand(ctx, c, id, false) }, Resolved: func(id string) error { return JournalCommand(ctx, c, id, true) }}
		mode := "live"
		if c.Manifest.Trial != nil {
			mode = c.Manifest.Trial.Mode
			source.Mode = mode
			source.Fixed = c.Manifest.Trial.Documents[c.StageID]
		}
		result = collector.ExecuteDurable(ctx, d, input, mode, source, collector.DurableOptions{Checkpoint: cp, Commit: func(ctx context.Context, rows []collector.Candidate, cp collector.Checkpoint) error {
			return CommitBatch(ctx, c, rows, cp)
		}})
	}()
	// Shutdown/lease loss leaves durable recovery to the next claim, never a blind action retry.
	stopped := ctx.Err() != nil
	cancel()
	<-heartbeatDone
	if stopped {
		return
	}
	finish, cancelFinish := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelFinish()
	if e := FinishWork(finish, c, result); e != nil {
		if v, ok := e.(*collector.Error); ok && (v.Code == "LEASE_LOST" || v.Code == "CONTROL_STOPPED" || v.Code == "CONTROL_CHANGED") {
			return
		}
		log.WithError(e).Warn("finish independent task item")
	}
}

func (w *TaskWorker) recoverCommands(parent context.Context, c *WorkClaim, browser *drission_rod.DrissionRod) {
	// Query only the original command/session. A lost receipt remains unknown.
	refs := unresolvedCommands(c.Attempt.CommandRefs)
	for _, id := range refs {
		if parent.Err() != nil {
			return
		}
		state := "receipt_unknown"
		var value any = map[string]string{"reason": "service_or_receipt_unavailable"}
		if browser != nil {
			ctx, cancel := context.WithTimeout(parent, 3*time.Second)
			receipt, e := browser.GetEditorCommand(ctx, c.Attempt.SessionID, id)
			cancel()
			if e == nil {
				state = "receipt_" + receipt.Status
				value = receipt
			}
		}
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		s := session(ctx)
		if e := s.Begin(); e == nil {
			r, e := getRun(s, c.Run.OwnerID, c.Run.ID, true)
			if e == nil {
				_, e = s.Exec(`UPDATE co_work_attempts SET command_refs=command_refs || CAST(? AS jsonb) WHERE id=?`, collector.JSON([]any{map[string]any{"command_id": id, "status": state, "receipt": value}}), c.Attempt.ID)
			}
			if e == nil {
				e = event(s, r, "original_receipt_queried", c.Item.ID, map[string]string{"command_id": id, "status": state})
			}
			if e == nil {
				e = s.Commit()
			}
			_ = s.Rollback()
		}
		s.Close()
		cancel()
	}
}
