package bootstrap

import (
	"context"
	"testing"

	"github.com/nekoimi/scrapio/internal/bean"
	"github.com/nekoimi/scrapio/internal/exportworker"
	"github.com/nekoimi/scrapio/internal/job"
	"github.com/nekoimi/scrapio/internal/qualityworker"
	"github.com/nekoimi/scrapio/internal/regressionworker"
	"github.com/nekoimi/scrapio/internal/runworker"
	"github.com/nekoimi/scrapio/internal/scheduleworker"
	"github.com/nekoimi/scrapio/internal/trialworker"
	"github.com/nekoimi/scrapio/internal/workflow"
)

// Exercise the production registration without loading configuration or starting
// PostgreSQL, browsers, schedulers or HTTP servers.
func TestControlWorkersHaveIndependentRegistryKeys(t *testing.T) {
	ctx := bean.ContextWithDefaultRegistry(context.Background())
	bean.MustRegister[bean.Lifecycle](ctx, bean.NewLifecycle("DB", nil, nil))
	registerControlWorkers(ctx)
	if bean.FromContext[bean.Lifecycle](ctx).Name() != "DB" {
		t.Fatal("background worker replaced the database lifecycle")
	}
	assertRegistered[job.WorkflowScheduler](t, ctx)
	assertRegistered[job.RecordExportWorker](t, ctx)
	assertRegistered[job.DocumentRetentionWorker](t, ctx)
	assertRegistered[trialworker.Worker](t, ctx)
	assertRegistered[runworker.Worker](t, ctx)
	assertRegistered[scheduleworker.Worker](t, ctx)
	assertRegistered[exportworker.Worker](t, ctx)
	assertRegistered[regressionworker.Worker](t, ctx)
	assertRegistered[qualityworker.Worker](t, ctx)
	assertRegistered[workflow.Worker](t, ctx)
}

func assertRegistered[T any](t *testing.T, ctx context.Context) {
	t.Helper()
	if worker := bean.PtrFromContext[T](ctx); worker == nil {
		t.Fatalf("worker %T has no independent registry key", worker)
	}
}
