// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package middleware

import (
	"net/http"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
)

type IdentityAuthMiddleware struct{ svc *svc.ServiceContext }

func NewIdentityAuthMiddleware(s *svc.ServiceContext) *IdentityAuthMiddleware {
	return &IdentityAuthMiddleware{svc: s}
}
func (m *IdentityAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := verifyActor(m.svc, r)
		if err != nil {
			httpapi.Error(r.Context(), w, err)
			return
		}
		// Restricted to atomic identity operations. Their SQL transaction either
		// validates the latest actor or recovers an exact completed operation receipt.
		// This does not authorize ordinary reads, code delivery or fresh stale writes.
		next(w, r.WithContext(httpapi.WithAuthorization(r.Context(), a)))
	}
}
func verifyActor(s *svc.ServiceContext, r *http.Request) (httpapi.Authorization, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > session.MaxTokenBytes+len("Bearer ") {
		return httpapi.Authorization{}, session.ErrInvalid
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return httpapi.Authorization{}, session.ErrInvalid
	}
	claims, id, err := s.Tokens.Parse(fields[1])
	if err != nil {
		return httpapi.Authorization{}, err
	}
	return httpapi.Authorization{Actor: userlogic.Actor{UserID: id, AuthVersion: claims.AuthVersion}, Claims: claims}, nil
}
