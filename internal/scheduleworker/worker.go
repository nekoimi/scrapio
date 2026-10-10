package scheduleworker

import (
	"context"
	"time"

	"github.com/nekoimi/scrapio/internal/repo/v22_schedule_repo"
	log "github.com/sirupsen/logrus"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22ScheduleWorker" }
func (w *Worker) Start(ctx context.Context) error {
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
				// Bounded tick batch keeps cancellation responsive even with many due schedules.
				for n := 0; n < 10; n++ {
					query, stop := context.WithTimeout(parent, 3*time.Second)
					worked, err := v22_schedule_repo.Tick(query)
					stop()
					if err != nil {
						log.WithError(err).Warn("v22 schedule tick failed")
						break
					}
					if !worked {
						break
					}
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
