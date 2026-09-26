package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

const (
	MaxScriptBytes        = 64 * 1024
	DefaultTimeout        = 2 * time.Second
	DefaultMaxInputBytes  = 512 * 1024
	DefaultMaxOutputBytes = 512 * 1024
)

type Request struct {
	Script         string
	Input          any
	Timeout        time.Duration
	MaxInputBytes  int
	MaxOutputBytes int
}

type Result struct{ Output any }

func Validate(source string) error {
	if strings.TrimSpace(source) == "" {
		return errors.New("script is required")
	}
	if len(source) > MaxScriptBytes {
		return errors.New("script exceeds 64 KiB limit")
	}
	_, err := goja.Compile("transform.js", "(function(input) {\n"+source+"\n})", false)
	return err
}

// Execute runs a pure JavaScript transform. Only the input value is exposed;
// no filesystem, network, process, timer, or Go host object is registered.
func Execute(ctx context.Context, request Request) (Result, error) {
	if err := Validate(request.Script); err != nil {
		return Result{}, err
	}
	if request.Timeout <= 0 {
		request.Timeout = DefaultTimeout
	}
	if request.MaxInputBytes <= 0 {
		request.MaxInputBytes = DefaultMaxInputBytes
	}
	if request.MaxOutputBytes <= 0 {
		request.MaxOutputBytes = DefaultMaxOutputBytes
	}
	input, err := json.Marshal(request.Input)
	if err != nil {
		return Result{}, fmt.Errorf("encode script input: %w", err)
	}
	if len(input) > request.MaxInputBytes {
		return Result{}, errors.New("script input exceeds size limit")
	}
	runtime := goja.New()
	var interrupted atomic.Bool
	// Materialize JSON inside the runtime instead of exposing caller-owned
	// Go maps or pointers as mutable host objects.
	inputValue, err := runtime.RunString("JSON.parse(" + strconv.Quote(string(input)) + ")")
	if err != nil {
		return Result{}, fmt.Errorf("decode script input: %w", err)
	}
	if err := runtime.Set("input", inputValue); err != nil {
		return Result{}, err
	}
	// Dynamic code loading is disabled; the supplied script is the only code
	// evaluated by this runtime.
	_ = runtime.Set("eval", nil)
	_ = runtime.Set("Function", nil)
	timer := time.AfterFunc(request.Timeout, func() { interrupted.Store(true); runtime.Interrupt("script timeout") })
	defer timer.Stop()
	if ctx == nil {
		ctx = context.Background()
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			runtime.Interrupt("script cancelled")
		case <-stop:
		}
	}()
	value, err := runtime.RunString("(function(input) {\n" + request.Script + "\n})(input)")
	if err != nil {
		if interrupted.Load() {
			return Result{}, context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("execute script: %w", err)
	}
	output := value.Export()
	encoded, err := json.Marshal(output)
	if err != nil {
		return Result{}, fmt.Errorf("encode script output: %w", err)
	}
	if len(encoded) > request.MaxOutputBytes {
		return Result{}, errors.New("script output exceeds size limit")
	}
	return Result{Output: output}, nil
}
