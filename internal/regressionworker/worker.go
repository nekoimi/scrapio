package regressionworker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nekoimi/scrapio/internal/publication"
	"github.com/nekoimi/scrapio/internal/regression"
	"github.com/nekoimi/scrapio/internal/repo/v22_regression_repo"
	log "github.com/sirupsen/logrus"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22RegressionWorker" }
func (w *Worker) Start(ctx context.Context) error {
	parent, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-tick.C:
			}
			query, stop := context.WithTimeout(parent, 3*time.Second)
			j, err := v22_regression_repo.Claim(query)
			stop()
			if err != nil {
				log.WithError(err).Warn("v22 regression claim failed")
				continue
			}
			if j == nil {
				continue
			}
			var snap regression.Snapshot
			var report regression.Report
			code := ""
			run, done := context.WithTimeout(parent, 30*time.Second)
			if j.SnapshotHash != publication.DefinitionHash(j.Snapshot) || json.Unmarshal([]byte(j.Snapshot), &snap) != nil {
				code = "SNAPSHOT_UNAVAILABLE"
			} else {
				report, err = regression.Execute(run, snap, func(n int) error {
					update, release := context.WithTimeout(run, time.Second)
					defer release()
					return v22_regression_repo.Progress(update, j, n)
				})
				if err != nil {
					code = "REGRESSION_INTERRUPTED"
					if err.Error() == "REPORT_BUDGET_EXCEEDED" {
						code = "REPORT_BUDGET_EXCEEDED"
					}
				}
			}
			done()
			persist, release := context.WithTimeout(context.Background(), 3*time.Second)
			err = v22_regression_repo.Finish(persist, j, report, code)
			release()
			if err != nil {
				log.WithError(err).Warn("v22 regression finish failed")
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
