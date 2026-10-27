package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
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

func JSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
func Error(ctx context.Context, w http.ResponseWriter, err error) {
	code := apperror.InternalError
	var tx *transaction.Failure
	var app *apperror.Error
	switch {
	case errors.As(err, &tx): // A cleanup/commit failure is never a normal business rejection.
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
	if d.status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="dexwallet"`)
	}
	if s, ok := w.(interface{ recordError(error, string) }); ok {
		s.recordError(err, string(code))
	}
	JSON(w, d.status, types.ErrorResponse{Code: string(code), Message: d.message, TraceId: TraceID(ctx)})
}
