// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package middleware

import (
	"net"
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

type AuthTransportMiddleware struct{ svc *svc.ServiceContext }

func NewAuthTransportMiddleware(s *svc.ServiceContext) *AuthTransportMiddleware {
	return &AuthTransportMiddleware{svc: s}
}
func (m *AuthTransportMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if m.svc.Auth == nil || m.svc.Tokens == nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.ServiceUnavailable, nil))
			return
		}
		// No trusted-proxy configuration exists yet. Never use client-supplied
		// X-Forwarded-For/X-Real-IP to bypass source rate limits.
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
			return
		}
		next(w, r.WithContext(httpapi.WithSource(r.Context(), host)))
	}
}
