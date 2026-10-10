package trialworker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/v22_trial_repo"
	"github.com/nekoimi/scrapio/internal/sample"
	"github.com/nekoimi/scrapio/internal/trial"
	log "github.com/sirupsen/logrus"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22TrialWorker" }
func (w *Worker) Start(ctx context.Context) error {
	browser := bean.PtrFromContext[drission_rod.DrissionRod](ctx)
	cfg := bean.PtrFromContext[config.Config](ctx)
	interrupted, err := v22_trial_repo.Interrupted()
	if err != nil {
		return err
	}
	for _, row := range interrupted {
		var input trial.Input
		_ = json.Unmarshal([]byte(row.Input), &input)
		// Interrupted live operations are never replayed: their effects may be unknown.
		if row.SessionId != "" && browser != nil {
			(&source{browser: browser, id: row.SessionId, plan: trial.Plan{EntryType: "web"}}).Close()
		}
		if err = v22_trial_repo.Finish(row.Id, trial.Summary{Status: "failed", Reason: "SERVICE_RESTARTED", FailedStep: row.CurrentStep, FailedStage: row.CurrentStage, DryRun: true, NetworkAccessed: input.Mode == "live", Warnings: []string{"INTERRUPTED_OPERATIONS_NOT_REPLAYED"}}); err != nil {
			return err
		}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				row, err := v22_trial_repo.Claim()
				if err != nil {
					log.WithError(err).Warn("claim v22 trial failed")
					continue
				}
				if row != nil {
					w.execute(runCtx, row, browser, cfg)
				}
			}
		}
	}()
	return nil
}
func (w *Worker) Stop(ctx context.Context) error {
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
func (w *Worker) execute(parent context.Context, row *table.V22Trial, browser *drission_rod.DrissionRod, cfg *config.Config) {
	defer func() {
		if recover() != nil {
			if err := v22_trial_repo.Finish(row.Id, trial.Summary{Status: "failed", Reason: "WORKER_PANIC", DryRun: true, NetworkAccessed: true}); err != nil {
				log.WithError(err).Error("persist failed v22 trial")
			}
		}
	}()
	var input trial.Input
	var fixed map[string]trial.FixedDocument
	plan, err := trial.Compile([]byte(row.Definition), row.EntryType)
	schema, schemaErr := output.DecodeSchema([]byte(row.OutputSchema))
	if err != nil || schemaErr != nil || json.Unmarshal([]byte(row.Input), &input) != nil || json.Unmarshal([]byte(row.FixedInputs), &fixed) != nil || input.Validate() != nil || row.DefinitionHash != capture.Hash(sample.CanonicalJSON([]byte(row.Definition))) {
		_ = v22_trial_repo.Finish(row.Id, trial.Summary{Status: "failed", Reason: "FROZEN_CONFIGURATION_INVALID", DryRun: true})
		return
	}
	if time.Since(row.CreatedAt) > 10*time.Minute {
		_ = v22_trial_repo.Finish(row.Id, trial.Summary{Status: "limited", Reason: "QUEUE_EXPIRED", DryRun: true})
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(input.Budget.Seconds)*time.Second)
	defer cancel()
	sessionID := uuid.NewString()
	if input.Mode == "live" && row.EntryType == "web" {
		if err = v22_trial_repo.SetSession(row.Id, sessionID); err != nil {
			_ = v22_trial_repo.Finish(row.Id, trial.Summary{Status: "failed", Reason: "SESSION_JOURNAL_FAILED", DryRun: true})
			return
		}
	}
	src := &source{browser: browser, cfg: cfg, ownerID: row.OwnerId, plan: plan, input: input, id: sessionID}
	// Cleanup also covers panics and failed creates; browser commands have bounded TTL.
	if input.Mode == "live" {
		defer src.Close()
	}
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := v22_trial_repo.Get(row.OwnerId, row.Id, "")
				if err != nil || current.CancelRequested || current.Status != "running" {
					cancel()
					return
				}
			}
		}
	}()
	runner := trial.Runner{Plan: plan, Input: input, Fixed: fixed, Source: src, Schema: schema,
		Existing: func(ctx context.Context, result extraction.Result) (map[string]output.Existing, error) {
			return v22_trial_repo.Existing(ctx, row, plan.Output, schema, result)
		},
		Emit: func(e trial.Event, doc *trial.Document) error { return v22_trial_repo.Progress(row.Id, e, doc) },
	}
	summary := runner.Run(ctx)
	if parent.Err() != nil && summary.Status == "cancelled" {
		summary.Status = "failed"
		summary.Reason = "SERVICE_STOPPED"
	}
	cancel()
	<-monitorDone
	if err = v22_trial_repo.Finish(row.Id, summary); err != nil {
		log.WithError(err).WithField("trial_id", row.Id).Error("persist v22 trial result failed")
	}
}
