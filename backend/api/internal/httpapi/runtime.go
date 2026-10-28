package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/rest/handler"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Runtime owns project admission and drain policy, not HTTP timeout or tracing machinery.
type Runtime struct {
	Config   config.Config
	Health   *health.Monitor
	Report   func(context.Context, Event)
	mu       sync.Mutex
	stopping bool
	active   sync.WaitGroup
	slots    syncx.Limit
}

func New(c config.Config, h *health.Monitor) *Runtime {
	return &Runtime{Config: c, Health: h, Report: reporter(c), slots: syncx.NewLimit(c.MaxConns)}
}
func (rt *Runtime) Stop() { rt.mu.Lock(); rt.stopping = true; rt.mu.Unlock(); rt.Health.Stop() }
func (rt *Runtime) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { rt.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Middleware covers routing failures as well as matched routes with one framework trace.
func (rt *Runtime) Middleware(next http.Handler) http.Handler {
	observed := rt.observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This entry serves JSON only. Protocol hints must not opt out of the
		// framework timeout (which deliberately skips SSE and WebSocket).
		if r.Header.Get("Upgrade") != "" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
			Error(r.Context(), w, apperror.New(apperror.InvalidArgument, nil))
			return
		}
		if r.ContentLength > rt.Config.MaxBytes {
			Error(r.Context(), w, apperror.New(apperror.PayloadTooLarge, nil))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, rt.Config.MaxBytes)
		next.ServeHTTP(w, r)
	}))
	traced := handler.TraceHandler(rt.Config.Name, "unmatched")(observed)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public clients cannot choose the server's correlation ID. Keep extraction and
		// span ownership in go-zero, clearing only propagation inputs at this boundary.
		r = r.Clone(trace.ContextWithSpanContext(r.Context(), trace.SpanContext{}))
		for _, name := range otel.GetTextMapPropagator().Fields() {
			r.Header.Del(name)
		}
		traced.ServeHTTP(w, r)
	})
}

// Work runs INSIDE go-zero's TimeoutHandler. A timed-out operation keeps its
// admission slot and participates in drain until the operation actually returns.
func (rt *Runtime) Work(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isHealth := r.URL.Path == LivePath || r.URL.Path == ReadyPath
		rt.mu.Lock()
		if !isHealth && (rt.stopping || !rt.Health.Ready()) {
			rt.mu.Unlock()
			Error(r.Context(), w, apperror.New(apperror.ServiceUnavailable, nil))
			return
		}
		tracked := !rt.stopping
		if !isHealth && !rt.slots.TryBorrow() {
			rt.mu.Unlock()
			Error(r.Context(), w, apperror.New(apperror.RateLimited, nil))
			return
		}
		if tracked {
			rt.active.Add(1)
		}
		rt.mu.Unlock()
		if tracked {
			defer rt.active.Done()
		}
		if !isHealth {
			defer func() {
				if err := rt.slots.Return(); err != nil {
					logx.WithContext(r.Context()).Error(err)
				}
			}()
		}
		// The stock recover handler writes an empty 500. Only the project error
		// conversion is adapted here; timeout and response buffering stay in go-zero.
		written := &statusWriter{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				failure := &panicError{cause: fmt.Errorf("panic: %v", p), stack: debug.Stack()}
				if written.wrote {
					// The framework owns the response buffer; there is no public reset.
					// Abort instead of appending an error to a partially written success.
					recordError(r.Context(), apperror.InternalError, http.StatusInternalServerError, failure)
					panic(http.ErrAbortHandler)
				}
				Error(r.Context(), w, failure)
			}
		}()
		next(written, r)
		// A canceled handler may return without writing. Ensure the framework's
		// completion/timeout race cannot turn that into an empty successful reply.
		if err := r.Context().Err(); err != nil && !written.wrote {
			Error(r.Context(), w, err)
		}
	}
}

type panicError struct {
	cause error
	stack []byte
}

func (e *panicError) Error() string { return e.cause.Error() }
func (e *panicError) Unwrap() error { return e.cause }
