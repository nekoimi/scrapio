package job

import (
	"context"
	"sync"
	"time"

	"github.com/nekoimi/scrapio/internal/repo/record_repo"
	log "github.com/sirupsen/logrus"
)

type RecordExportWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewRecordExportWorker() *RecordExportWorker { return &RecordExportWorker{} }
func (*RecordExportWorker) Name() string         { return "RecordExportWorker" }
func (w *RecordExportWorker) Start(parent context.Context) error {
	if err := record_repo.RecoverExports(); err != nil {
		return err
	}
	if err := record_repo.CleanupExports(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		lastCleanup := time.Now()
		for {
			if time.Since(lastCleanup) > 24*time.Hour {
				if err := record_repo.CleanupExports(); err != nil {
					log.Errorf("清理过期导出失败: %v", err)
				}
				lastCleanup = time.Now()
			}
			processed, err := record_repo.ProcessNextExport(ctx)
			if err != nil && ctx.Err() == nil {
				log.Errorf("记录导出任务失败: %v", err)
			}
			if processed {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}
func (w *RecordExportWorker) Stop(context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	return nil
}
