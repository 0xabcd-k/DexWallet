package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/logx"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Event struct {
	TraceID, Method, Route, Code string
	Status                       int
	Duration                     time.Duration
	Cause                        error
	Stack                        []byte
}
type Runtime struct {
	Config   config.Config
	Health   *health.Monitor
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
	Report   func(context.Context, Event)
	mu       sync.Mutex
	stopping bool
	active   sync.WaitGroup
	slots    chan struct{}
	Routes   map[string]string
}

func New(c config.Config, h *health.Monitor) *Runtime {
	// Local provider always creates valid IDs; no exporter or global state needed.
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	rt := &Runtime{Config: c, Health: h, provider: provider, tracer: provider.Tracer(c.Name), slots: make(chan struct{}, c.MaxInFlight), Routes: map[string]string{}}
	rt.Report = func(ctx context.Context, e Event) {
		fields := []logx.LogField{logx.Field("service", c.Name), logx.Field("environment", c.Environment), logx.Field("traceId", e.TraceID), logx.Field("method", e.Method), logx.Field("route", e.Route), logx.Field("httpStatus", e.Status), logx.Field("code", e.Code), logx.Field("durationMs", e.Duration.Milliseconds())}
		if e.Cause != nil {
			fields = append(fields, logx.Field("cause", e.Cause.Error()))
		}
		var tx *transaction.Failure
		if errors.As(e.Cause, &tx) && len(tx.Stack) > 0 {
			e.Stack = tx.Stack
		}
		if len(e.Stack) > 0 {
			fields = append(fields, logx.Field("stack", string(e.Stack)))
		}
		logger := logx.WithContext(ctx)
		switch {
		case e.Status >= 500:
			logger.Errorw("http request", fields...)
		case e.Duration >= c.RequestTimeout*4/5:
			logger.Sloww("http request", fields...)
		case e.Route != LivePath && e.Route != ReadyPath:
			logger.Infow("http request", fields...)
		}
	}
	return rt
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
func (rt *Runtime) Close(ctx context.Context) error { return rt.provider.Shutdown(ctx) }

// Middleware surrounds the router, so 404/405 share the same context and exits.
func (rt *Runtime) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		// This public entry is not a trusted distributed-tracing boundary. Start a root
		// rather than reflecting caller-supplied tracing headers into our correlation ID.
		ctx, span := rt.tracer.Start(r.Context(), "http request", trace.WithNewRoot())
		defer span.End()
		ctx, cancel := context.WithTimeout(ctx, rt.Config.RequestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set(TraceHeader, TraceID(ctx))
		route := rt.Routes[r.Method+" "+r.URL.Path]
		if route == "" {
			route = "unmatched"
		}
		event := Event{TraceID: TraceID(ctx), Method: r.Method, Route: route}
		finish := func(b *buffer) {
			event.Status = b.status
			if event.Status == 0 {
				event.Status = 200
			}
			event.Code = b.code
			if event.Code == "" {
				event.Code = SuccessCode
			}
			event.Cause = b.cause
			event.Stack = b.stack
			event.Duration = time.Since(started)
			for k, v := range b.header {
				w.Header()[k] = v
			}
			w.Header().Set(TraceHeader, TraceID(ctx))
			w.WriteHeader(event.Status)
			_, _ = w.Write(b.body.Bytes())
			rt.Report(ctx, event)
		}
		reject := func(code apperror.Code, cause error) {
			b := newBuffer()
			Error(ctx, b, apperror.New(code, cause))
			finish(b)
		}
		isHealth := r.URL.Path == LivePath || r.URL.Path == ReadyPath
		if !isHealth && !rt.Health.Ready() {
			reject(apperror.ServiceUnavailable, nil)
			return
		}
		rt.mu.Lock()
		if rt.stopping && !isHealth {
			rt.mu.Unlock()
			reject(apperror.ServiceUnavailable, nil)
			return
		}
		// No new jobs after Stop; health requests during draining are handled inline.
		if rt.stopping {
			rt.mu.Unlock()
			b := newBuffer()
			next.ServeHTTP(b, r)
			finish(b)
			return
		}
		if !isHealth {
			select {
			case rt.slots <- struct{}{}:
			default:
				rt.mu.Unlock()
				reject(apperror.RateLimited, nil)
				return
			}
		}
		rt.active.Add(1)
		rt.mu.Unlock()
		if r.ContentLength > rt.Config.MaxBodyBytes {
			if !isHealth {
				<-rt.slots
			}
			rt.active.Done()
			reject(apperror.PayloadTooLarge, nil)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, rt.Config.MaxBodyBytes)
		b := newBuffer()
		done := make(chan struct{})
		go func() {
			defer rt.active.Done()
			if !isHealth {
				defer func() { <-rt.slots }()
			}
			defer close(done)
			defer func() {
				if p := recover(); p != nil {
					b = newBuffer()
					b.stack = debug.Stack()
					Error(ctx, b, fmt.Errorf("panic: %v", p))
				}
				if ctx.Err() != nil && b.cause != nil {
					rt.Report(ctx, Event{TraceID: TraceID(ctx), Method: r.Method, Route: route, Code: b.code, Status: b.status, Duration: time.Since(started), Cause: b.cause, Stack: b.stack})
				}
			}()
			next.ServeHTTP(b, r)
		}()
		select {
		case <-done:
			if ctx.Err() != nil {
				reject(apperror.RequestTimeout, ctx.Err())
			} else {
				finish(b)
			}
		case <-ctx.Done():
			reject(apperror.RequestTimeout, ctx.Err())
			// The worker owns its buffer until completion. Never read/write it here.
		}
	})
}

type buffer struct {
	header http.Header
	body   bytes.Buffer
	status int
	code   string
	cause  error
	stack  []byte
}

func newBuffer() *buffer              { return &buffer{header: make(http.Header)} }
func (b *buffer) Header() http.Header { return b.header }
func (b *buffer) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *buffer) Write(data []byte) (int, error) {
	if b.body.Len()+len(data) > 4<<20 {
		panic("JSON response exceeds 4 MiB")
	}
	if b.status == 0 {
		b.status = 200
	}
	return b.body.Write(data)
}
func (b *buffer) recordError(err error, code string) { b.cause = err; b.code = code }
