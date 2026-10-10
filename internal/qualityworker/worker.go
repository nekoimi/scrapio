package qualityworker

import (
	"context"
	"github.com/nekoimi/scrapio/internal/repo/v22_quality_repo"
	log "github.com/sirupsen/logrus"
	"time"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22QualityWorker" }
func (w *Worker) Start(ctx context.Context) error {
	parent, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var cursor int64
		for {
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
			}
			read, stop := context.WithTimeout(parent, 3*time.Second)
			err := v22_quality_repo.ProcessOne(read)
			stop()
			if err != nil {
				log.WithError(err).Warn("v22 quality observation failed")
			}
			read, stop = context.WithTimeout(parent, 3*time.Second)
			next, err := v22_quality_repo.ScanSchedules(read, cursor)
			stop()
			if err == nil {
				cursor = next
			} else {
				log.WithError(err).Warn("v22 schedule quality check failed")
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
