package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Event struct {
	TraceID, Method, Route, Code string
	Status                       int
	Duration                     time.Duration
	Cause                        error
	Stack                        []byte
}
type requestLogKey struct{}
type requestLog struct {
	mu       sync.Mutex
	event    Event
	started  time.Time
	returned bool
	report   func(context.Context, Event)
}

func reporter(c config.Config) func(context.Context, Event) {
	return func(ctx context.Context, e Event) {
		if !c.Middlewares.Log {
			return
		}
		fields := []logx.LogField{logx.Field("service", c.Name), logx.Field("environment", c.Environment), logx.Field("traceId", e.TraceID), logx.Field("method", e.Method), logx.Field("route", e.Route), logx.Field("httpStatus", e.Status), logx.Field("code", e.Code), logx.Field("durationMs", e.Duration.Milliseconds())}
		if e.Cause != nil {
			fields = append(fields, logx.Field("cause", e.Cause.Error()))
		}
		if len(e.Stack) > 0 {
			fields = append(fields, logx.Field("stack", string(e.Stack)))
		}
		logger := logx.WithContext(ctx)
		switch {
		case e.Status >= 500:
			logger.Errorw("http request", fields...)
		case e.Duration >= c.RequestDuration()*4/5:
			logger.Sloww("http request", fields...)
		case e.Route != LivePath && e.Route != ReadyPath:
			logger.Infow("http request", fields...)
		}
	}
}
func (rt *Runtime) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &requestLog{started: time.Now(), report: rt.Report, event: Event{TraceID: TraceID(r.Context()), Method: r.Method, Route: "unmatched", Code: SuccessCode}}
		ctx := context.WithValue(r.Context(), requestLogKey{}, state)
		w.Header().Set(TraceHeader, state.event.TraceID)
		observed := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			state.mu.Lock()
			state.returned = true
			event := state.event
			state.mu.Unlock()
			if observed.wrote || event.Status == 0 {
				event.Status = observed.status
			}
			event.Duration = time.Since(state.started)
			rt.Report(ctx, event)
		}()
		next.ServeHTTP(observed, r.WithContext(ctx))
	})
}

// Route is attached when the router matches a template, not by looking up the
// concrete URL in a map (which cannot match /resources/:idx).
func Route(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if state, ok := r.Context().Value(requestLogKey{}).(*requestLog); ok {
			state.mu.Lock()
			state.event.Route = path
			state.mu.Unlock()
		}
		span := trace.SpanFromContext(r.Context())
		span.SetName(path)
		span.SetAttributes(attribute.String("http.route", path))
		next.ServeHTTP(w, r)
	})
}
func recordError(ctx context.Context, code apperror.Code, status int, err error) {
	state, ok := ctx.Value(requestLogKey{}).(*requestLog)
	if !ok {
		return
	}
	state.mu.Lock()
	event := state.event
	event.Code = string(code)
	event.Status = status
	event.Cause = err
	var panicFailure *panicError
	var tx *transaction.Failure
	if errors.As(err, &panicFailure) {
		event.Stack = panicFailure.stack
	} else if errors.As(err, &tx) {
		event.Stack = tx.Stack
	}
	// A worker may fail after its timeout response. Preserve the original response
	// summary and emit the late diagnostic once, with the same trace ID.
	late := state.returned || state.event.Code == string(apperror.RequestTimeout)
	if !late {
		state.event = event
	}
	state.mu.Unlock()
	if late && code != apperror.RequestTimeout {
		event.Duration = time.Since(state.started)
		state.report(ctx, event)
	}
}

// No response body is buffered here. Unwrap keeps ResponseController usable.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
