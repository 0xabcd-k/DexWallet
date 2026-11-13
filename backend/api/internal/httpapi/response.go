package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	accountlogic "github.com/0xabcd-k/DexWallet/backend/logic/account"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	transferlogic "github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
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
	apperror.PaymentRestricted:            {423, "Payments temporarily restricted"},
	apperror.TransferAmountLimit:          {422, "Transfer amount outside plan limits"},
	apperror.TransferDailyLimit:           {422, "Daily transfer limit exceeded"},
	apperror.InsufficientBalance:          {422, "Insufficient available balance"},
	apperror.TransferRequestConflict:      {409, "Request key parameters differ"},
	apperror.TransferOutcomeUnknown:       {503, "Transfer outcome unknown; recover using original key"},
	apperror.TransferBusy:                 {409, "Transfer busy; retry using original key"},
	apperror.TransferRecipientUnavailable: {404, "Transfer recipient unavailable"},
	apperror.IdentityHistoryUnknown:       {409, "Identity history requires verified backfill"},

	apperror.AuthProofInvalid:        {400, "Invalid or expired authentication proof"},
	apperror.IdentityOccupied:        {409, "Identity belongs to another account"},
	apperror.AnotherIdentityRequired: {422, "Bind another identity first"},
	apperror.IdentityAlreadyBound:    {409, "Identity type already bound; use replace"},
	apperror.InvalidArgument:         {400, "Invalid argument"}, apperror.Unauthenticated: {401, "Authentication required"}, apperror.PermissionDenied: {403, "Permission denied"}, apperror.NotFound: {404, "Not found"}, apperror.MethodNotAllowed: {405, "Method not allowed"}, apperror.Conflict: {409, "Conflict"}, apperror.Unprocessable: {422, "Request cannot be processed"}, apperror.RateLimited: {429, "Too many requests"}, apperror.InternalError: {500, "Internal error"}, apperror.ServiceUnavailable: {503, "Service unavailable"}, apperror.RequestTimeout: {503, "Request timed out; execution outcome may be unknown"}, apperror.PayloadTooLarge: {413, "Payload too large"},
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
	// Generated response DTOs are JSON-compatible; serialization belongs to httpx.
	// Renewal is attached to successful business responses after cancellation checks.
	// Identity operations carry their own authoritative token.
	if status >= 200 && status < 300 {
		switch reply := value.(type) {
		case *types.TokenResponse:
			w.Header().Set(session.TokenHeader, reply.Data.AccessToken)
		default:
			if raw := renewal(ctx); raw != "" {
				w.Header().Set(session.TokenHeader, raw)
			}
			if a, err := Authorized(ctx); err == nil && a.Renewal != "" {
				w.Header().Set(session.TokenHeader, a.Renewal)
			}
		}
	}
	httpx.WriteJsonCtx(ctx, w, status, value)
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
	case errors.Is(err, transferlogic.ErrOutcomeUnknown):
		code = apperror.TransferOutcomeUnknown
	case errors.As(err, &tx): // Cleanup/commit failures cannot become business rejections.
	case errors.Is(err, transferlogic.ErrRestricted):
		code = apperror.PaymentRestricted
	case errors.Is(err, transferlogic.ErrAmountLimit):
		code = apperror.TransferAmountLimit
	case errors.Is(err, transferlogic.ErrDailyLimit):
		code = apperror.TransferDailyLimit
	case errors.Is(err, transferlogic.ErrRequestConflict):
		code = apperror.TransferRequestConflict
	case errors.Is(err, transferlogic.ErrBusy):
		code = apperror.TransferBusy
	case errors.Is(err, transferlogic.ErrRecipientUnavailable):
		code = apperror.TransferRecipientUnavailable
	case errors.Is(err, transferlogic.ErrNotFound):
		code = apperror.NotFound
	case errors.Is(err, transferlogic.ErrInvalidRequest):
		code = apperror.InvalidArgument
	case errors.Is(err, accountlogic.ErrInsufficientBalance):
		code = apperror.InsufficientBalance
	case errors.Is(err, userlogic.ErrHistoryUnknown):
		code = apperror.IdentityHistoryUnknown
	case errors.As(err, &oversized):
		code = apperror.PayloadTooLarge
	case errors.As(err, &app):
		code = app.Code
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = apperror.RequestTimeout
	case errors.Is(err, authlogic.ErrUnavailable):
		code = apperror.ServiceUnavailable
	case errors.Is(err, session.ErrInvalid), errors.Is(err, userlogic.ErrStaleSession):
		code = apperror.Unauthenticated
	case errors.Is(err, userlogic.ErrDisabled):
		code = apperror.PermissionDenied
	case errors.Is(err, authlogic.ErrInvalid), errors.Is(err, userlogic.ErrInvalidInput):
		code = apperror.InvalidArgument
	case errors.Is(err, authlogic.ErrCode), errors.Is(err, authlogic.ErrTelegram):
		code = apperror.AuthProofInvalid
	case errors.Is(err, authlogic.ErrRateLimited):
		code = apperror.RateLimited
	case errors.Is(err, userlogic.ErrOccupied):
		code = apperror.IdentityOccupied
	case errors.Is(err, userlogic.ErrNeedSecondIdentity):
		code = apperror.AnotherIdentityRequired
	case errors.Is(err, userlogic.ErrUseReplace):
		code = apperror.IdentityAlreadyBound
	case errors.Is(err, authlogic.ErrReplay), errors.Is(err, userlogic.ErrBindingChanged), errors.Is(err, userlogic.ErrConflict):
		code = apperror.Conflict
	}
	d, ok := descriptions[code]
	if !ok {
		code = apperror.InternalError
		d = descriptions[code]
	}
	return code, d
}
