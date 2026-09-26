package job

import (
	"context"
	"time"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/repo/task_repo"
	log "github.com/sirupsen/logrus"
)

type DocumentRetentionWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewDocumentRetentionWorker() *DocumentRetentionWorker { return &DocumentRetentionWorker{} }
func (w *DocumentRetentionWorker) Name() string            { return "DocumentRetentionWorker" }
func (w *DocumentRetentionWorker) Start(parent context.Context) error {
	cfg := bean.PtrFromContext[config.Config](parent)
	if cfg == nil || cfg.Retention == nil || cfg.Retention.DocumentDays <= 0 {
		return nil
	}
	days := cfg.Retention.DocumentDays
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cutoff := time.Now().AddDate(0, 0, -days)
				for i := 0; i < 10; i++ {
					count, err := task_repo.CleanupExpiredDocuments(cutoff)
					if err != nil {
						log.Warnf("document retention cleanup failed: %v", err)
						break
					}
					if count < 100 {
						break
					}
				}
			}
		}
	}()
	return nil
}
func (w *DocumentRetentionWorker) Stop(context.Context) error {
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
	return nil
}
