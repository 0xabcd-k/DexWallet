package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/rest/httpx"
	"go.opentelemetry.io/otel/trace"
)

const (
	SuccessCode = "OK"
	TraceHeader = "X-Trace-Id"
	LivePath    = "/health/live"
	ReadyPath   = "/health/ready"
)

func TraceID(ctx context.Context) string { return trace.SpanContextFromContext(ctx).TraceID().String() }

type description struct {
	status  int
	message string
}

var descriptions = map[apperror.Code]description{
	apperror.InvalidArgument: {400, "Invalid argument"}, apperror.Unauthenticated: {401, "Authentication required"}, apperror.PermissionDenied: {403, "Permission denied"}, apperror.NotFound: {404, "Not found"}, apperror.MethodNotAllowed: {405, "Method not allowed"}, apperror.Conflict: {409, "Conflict"}, apperror.Unprocessable: {422, "Request cannot be processed"}, apperror.RateLimited: {429, "Too many requests"}, apperror.InternalError: {500, "Internal error"}, apperror.ServiceUnavailable: {503, "Service unavailable"}, apperror.RequestTimeout: {503, "Request timed out; execution outcome may be unknown"}, apperror.PayloadTooLarge: {413, "Payload too large"},
}

// ConfigureErrors installs one process-wide stateless adapter. Per-request
// diagnostics live in context, so concurrent servers never replace callbacks.
var configureErrors sync.Once

func ConfigureErrors() {
	configureErrors.Do(func() {
		httpx.SetErrorHandlerCtx(func(ctx context.Context, err error) (int, any) {
			code, d := describeError(err)
			recordError(ctx, code, d.status, err)
			return d.status, types.ErrorResponse{Code: string(code), Message: d.message, TraceId: TraceID(ctx)}
		})
	})
}

func JSON(ctx context.Context, w http.ResponseWriter, status int, value any) {
	if err := ctx.Err(); err != nil {
		Error(ctx, w, err)
		return
	}
	// Preflight preserves the JSON error contract even for invalid values: httpx's
	// own marshal-failure fallback is plain text. All HTTP output stays in httpx.
	data, err := json.Marshal(value)
	if err != nil {
		Error(ctx, w, err)
		return
	}
	httpx.WriteJsonCtx(ctx, w, status, json.RawMessage(data))
}
func Error(ctx context.Context, w http.ResponseWriter, err error) {
	_, d := describeError(err)
	if d.status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="dexwallet"`)
	}
	httpx.ErrorCtx(ctx, w, err)
}
func describeError(err error) (apperror.Code, description) {
	code := apperror.InternalError
	var tx *transaction.Failure
	var app *apperror.Error
	var oversized *http.MaxBytesError
	switch {
	case errors.As(err, &tx): // Cleanup/commit failures cannot become business rejections.
	case errors.As(err, &oversized):
		code = apperror.PayloadTooLarge
	case errors.As(err, &app):
		code = app.Code
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = apperror.RequestTimeout
	}
	d, ok := descriptions[code]
	if !ok {
		code = apperror.InternalError
		d = descriptions[code]
	}
	return code, d
}
