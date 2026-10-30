package middleware

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"net"
	"net/http"
)

type BackofficeTransportMiddleware struct{ svc *svc.ServiceContext }

func NewBackofficeTransportMiddleware(s *svc.ServiceContext) *BackofficeTransportMiddleware {
	return &BackofficeTransportMiddleware{svc: s}
}
func (m *BackofficeTransportMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if m.svc.Backoffice == nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.ServiceUnavailable, nil))
			return
		}
		source, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
			return
		}
		next(w, r.WithContext(httpapi.WithSource(r.Context(), source)))
	}
}
