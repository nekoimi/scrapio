package exportworker

import (
	"context"
	"time"

	"github.com/nekoimi/scrapio/internal/dataquery"
	"github.com/nekoimi/scrapio/internal/output"
	"github.com/nekoimi/scrapio/internal/repo/v22_query_repo"
	log "github.com/sirupsen/logrus"
)

type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New() *Worker           { return &Worker{} }
func (*Worker) Name() string { return "v22ExportWorker" }
func (w *Worker) Start(ctx context.Context) error {
	parent, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		nextCleanup := time.Time{}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
				query, stop := context.WithTimeout(parent, 3*time.Second)
				var err error
				if time.Now().After(nextCleanup) {
					err = v22_query_repo.Cleanup(query)
					if err == nil {
						nextCleanup = time.Now().Add(time.Minute)
					}
				}
				stop()
				if err != nil {
					log.WithError(err).Warn("v22 export recovery failed")
					continue
				}
				query, stop = context.WithTimeout(parent, 3*time.Second)
				job, err := v22_query_repo.Claim(query)
				stop()
				if err != nil {
					log.WithError(err).Warn("v22 export claim failed")
					continue
				}
				if job == nil {
					continue
				}
				render, done := context.WithTimeout(parent, 15*time.Second)
				schema, err := output.DecodeSchema([]byte(job.Schema))
				q, qe := dataquery.Decode(job.Query)
				rows, re := v22_query_repo.ExportRows(render, job)
				code := ""
				var file []byte
				if err != nil || qe != nil || re != nil {
					code = "SNAPSHOT_UNAVAILABLE"
				} else {
					count := 0
					file, err = dataquery.Render(job.Format, schema, q, rows, func() error {
						if err := render.Err(); err != nil {
							return err
						}
						count++
						if count%100 == 0 || count == 1 {
							return v22_query_repo.Progress(render, job, count-1)
						}
						return nil
					})
					if err != nil {
						code = "EXPORT_RENDER_FAILED"
					}
				}
				done()
				persist, release := context.WithTimeout(context.Background(), 3*time.Second)
				if err := v22_query_repo.Finish(persist, job, file, code); err != nil {
					log.WithError(err).Warn("v22 export finish failed")
				}
				release()
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
