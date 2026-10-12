package bean

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"
)

func TestDatabaseFailureStopsOtherLifecycles(t *testing.T) {
	migrationErr := errors.New("migration failed")
	started := false
	m := &LifecycleManager{ctx: context.Background(), lifecycles: []Lifecycle{
		NewLifecycle("DB", func(context.Context) error { return migrationErr }, func(context.Context) error { return nil }),
		NewLifecycle("Worker", func(context.Context) error { started = true; return nil }, func(context.Context) error { return nil }),
	}}
	err := m.StartAndServe()
	if !errors.Is(err, migrationErr) || !strings.Contains(err.Error(), "DB") {
		t.Fatalf("expected database startup error, got %v", err)
	}
	if started {
		t.Fatal("worker started before database migration completed")
	}
}

type firstWorker struct{ Lifecycle }
type secondWorker struct{ Lifecycle }

func TestConcreteWorkersAllStartAfterDatabase(t *testing.T) {
	ctx := ContextWithDefaultRegistry(context.Background())
	m := LifecycleFromContext(ctx)
	defer signal.Stop(m.sigs)
	started := make(chan string, 3)
	stopped := make(chan string, 3)
	lifecycle := func(name string) Lifecycle {
		return NewLifecycle(name, func(context.Context) error { started <- name; return nil }, func(context.Context) error { stopped <- name; return nil })
	}
	MustRegister[Lifecycle](ctx, lifecycle("DB"))
	MustRegisterPtr[firstWorker](ctx, &firstWorker{lifecycle("Trial")})
	MustRegisterPtr[secondWorker](ctx, &secondWorker{lifecycle("Formal")})
	if len(m.lifecycles) != 3 {
		t.Fatalf("expected all three startup hooks, got %d", len(m.lifecycles))
	}
	done := make(chan error, 1)
	go func() { done <- m.StartAndServe() }()
	defer func() { m.sigs <- os.Interrupt }()
	order := []string{}
	for len(order) < 3 {
		select {
		case name := <-started:
			order = append(order, name)
		case <-time.After(time.Second):
			t.Fatal("not all registered workers started")
		}
	}
	if order[0] != "DB" {
		t.Fatal("workers started before database", order)
	}
	m.sigs <- os.Interrupt
	select {
	case err := <-done:
		if err != nil || len(stopped) != 3 {
			t.Fatalf("shutdown failed: %v, stopped=%d", err, len(stopped))
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle shutdown blocked")
	}
}
