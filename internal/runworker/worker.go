package runworker

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/db/table"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/extraction"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/repo/v22_run_repo"
	"github.com/nekoimi/scrapio/internal/repo/v22_trial_repo"
	runmodel "github.com/nekoimi/scrapio/internal/run"
	"github.com/nekoimi/scrapio/internal/trial"
	"github.com/nekoimi/scrapio/internal/trialworker"
	log "github.com/sirupsen/logrus"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22RunWorker" }
func (w *Worker) Start(ctx context.Context) error {
	browser := bean.PtrFromContext[drission_rod.DrissionRod](ctx)
	cfg := bean.PtrFromContext[config.Config](ctx)
	parent, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
				query, stop := context.WithTimeout(parent, 5*time.Second)
				expired, err := v22_run_repo.Recover(query)
				stop()
				if err != nil {
					log.WithError(err).Warn("recover v22 formal runs failed")
					continue
				}
				for _, row := range expired {
					if row.SessionId != "" && browser != nil {
						trialworker.NewSource(browser, cfg, row.OwnerId, trial.Plan{EntryType: "web"}, trial.Input{}, row.SessionId).Close()
					}
				}
				query, stop = context.WithTimeout(parent, 5*time.Second)
				row, err := v22_run_repo.Claim(query)
				stop()
				if err != nil {
					log.WithError(err).Warn("claim v22 formal run failed")
					continue
				}
				if row != nil {
					w.execute(parent, row, browser, cfg)
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
func persist(row *table.V22Run, summary trial.Summary) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := v22_run_repo.Finish(ctx, row.Id, row.LeaseToken, summary); err != nil {
		log.WithError(err).WithField("run_id", row.Id).Error("persist v22 formal run failed")
		// May have committed despite a lost response. Finish is terminal-idempotent.
		fallback, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = v22_run_repo.Finish(fallback, row.Id, row.LeaseToken, trial.Summary{Status: "failed", Reason: "PERSISTENCE_FAILED", NetworkAccessed: summary.NetworkAccessed, Warnings: []string{"QUERY_RUN_FOR_COMMIT_OUTCOME"}})
	}
}
func (w *Worker) execute(parent context.Context, row *table.V22Run, browser *drission_rod.DrissionRod, cfg *config.Config) {
	defer func() {
		if recover() != nil {
			persist(row, trial.Summary{Status: "failed", Reason: "WORKER_PANIC", NetworkAccessed: true, Warnings: []string{}})
		}
	}()
	plan, schema, err := v22_run_repo.VersionPlan(&table.V22Version{Definition: row.Definition, DefinitionHash: row.DefinitionHash, EntryType: row.EntryType, OutputSchema: row.OutputSchema, ContractVersion: row.PublicationContract, InterpreterVersion: row.InterpreterVersion})
	var input runmodel.Input
	if err != nil || runmodel.Decode(row.Input, &input) != nil || input.Validate() != nil || input.VersionID != row.VersionId || trial.ValidateScope(plan, input.Trial(row.CollectorRevision)) != nil {
		persist(row, trial.Summary{Status: "failed", Reason: "FROZEN_CONFIGURATION_INVALID", Warnings: []string{}})
		return
	}
	if time.Since(row.CreatedAt) > 45*time.Minute {
		persist(row, trial.Summary{Status: "failed", Reason: "QUEUE_EXPIRED", Warnings: []string{}})
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(input.Budget.Seconds)*time.Second)
	defer cancel()
	// Recheck protocol and credentials at execution time, before target access.
	if plan.EntryType == "web" {
		probe, stop := context.WithTimeout(ctx, 4*time.Second)
		caps := publication.Capabilities{Contract: publication.ContractVersion, Interpreter: extraction.InterpreterVersion, BrowserProtocol: drission_rod.EditorProtocolVersion, Actions: editor.Actions}
		if browser != nil {
			caps.Session = browser.ProbeEditor(probe) == nil
			caps.Commands = browser.ProbeEditorCommands(probe) == nil
			caps.Snapshot = browser.ProbeEditorSnapshot(probe) == nil
			if plan.CredentialRef != "" {
				caps.CredentialReady = browser.ProbeEditorAuthorization(probe) == nil
			}
		}
		stop()
		if !publication.CheckCapabilities(plan, caps).Ready {
			persist(row, trial.Summary{Status: "failed", Reason: "BROWSER_CAPABILITY_UNAVAILABLE", Warnings: []string{}})
			return
		}
	}
	sessionID := uuid.NewString()
	if row.EntryType == "web" {
		journal, stop := context.WithTimeout(ctx, 5*time.Second)
		err = v22_run_repo.SetSession(journal, row.Id, row.LeaseToken, sessionID)
		stop()
		if err != nil {
			persist(row, trial.Summary{Status: "failed", Reason: "SESSION_JOURNAL_FAILED", Warnings: []string{}})
			return
		}
	}
	src := trialworker.NewSource(browser, cfg, row.OwnerId, plan, input.Trial(row.CollectorRevision), sessionID)
	defer src.Close()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		renew := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				query, stop := context.WithTimeout(ctx, 3*time.Second)
				current, err := v22_run_repo.Get(query, row.OwnerId, row.Id, "")
				stop()
				if err != nil || current.Status != "running" || current.CancelRequested || current.LeaseToken != row.LeaseToken {
					cancel()
					return
				}
				renew++
				if renew >= 10 {
					query, stop = context.WithTimeout(ctx, 3*time.Second)
					err = v22_run_repo.Renew(query, row.Id, row.LeaseToken)
					stop()
					renew = 0
					if err != nil {
						cancel()
						return
					}
				}
			}
		}
	}()
	runner := trial.Runner{Continuous: true, Plan: plan, Input: input.Trial(row.CollectorRevision), Source: src, Schema: schema,
		Existing: func(ctx context.Context, result extraction.Result) (map[string]output.Existing, error) {
			return v22_trial_repo.Existing(ctx, &table.V22Trial{OwnerId: row.OwnerId}, plan.Output, schema, result)
		},
		Emit: func(e trial.Event, doc *trial.Document) error {
			if doc == nil && e.Stage == "output" && runmodel.Terminal(e.Status) {
				e.Status = "running"
				e.Code = "OUTPUT_PREPARED"
			}
			query, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			return v22_run_repo.Progress(query, row.Id, row.LeaseToken, e, doc)
		},
	}
	summary := runner.Run(ctx)
	if parent.Err() != nil {
		summary.Status = "failed"
		summary.Reason = "SERVICE_STOPPED"
		summary.Output = nil
	}
	cancel()
	<-monitorDone
	persist(row, summary)
}
