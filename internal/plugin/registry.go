package plugin

import (
	"context"
	"errors"
	"sync"
)

type Task struct {
	ID             int64
	RecordID       int64
	IdempotencyKey string
	ResourceID     int64
	EventType      string
	Input          map[string]any
}

// RecordHandler explicitly opts in to record subscriptions. URL input is mapped
// from a scalar dataset field; generic handlers can implement their own contract.
type RecordHandler interface{ SupportsRecordEvents() bool }

type Handler interface {
	Code() string
	Capabilities() []string
	Health(context.Context) error
	Handle(context.Context, Task) (output any, externalID string, err error)
}

// AsyncHandler is a plugin whose Handle call starts an external operation.
// The worker persists the returned external ID and calls Poll until done.
type AsyncHandler interface {
	Handler
	Poll(context.Context, Task, string) (output any, done bool, err error)
}

// CompletionHandler receives the final external output before the plugin task
// is marked successful. It is useful for writing provider artifacts back to a
// resource without coupling the generic worker to a specific provider.
type CompletionHandler interface {
	OnComplete(context.Context, Task, any) error
}

// PermanentError marks an error as a terminal business failure. Transport and
// temporary provider errors remain retryable by default.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string {
	if e == nil || e.Err == nil {
		return "permanent plugin error"
	}
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRegistry() *Registry { return &Registry{handlers: map[string]Handler{}} }

func (r *Registry) Register(handler Handler) error {
	if handler == nil || handler.Code() == "" {
		return errors.New("plugin code is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[handler.Code()]; exists {
		return errors.New("plugin already registered")
	}
	r.handlers[handler.Code()] = handler
	return nil
}

func (r *Registry) Get(code string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[code]
	return handler, ok
}

func (r *Registry) List() []Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Handler, 0, len(r.handlers))
	for _, handler := range r.handlers {
		result = append(result, handler)
	}
	return result
}
