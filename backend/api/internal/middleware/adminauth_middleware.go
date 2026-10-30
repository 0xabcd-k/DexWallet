package middleware

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"net/http"
)

type AdminAuthMiddleware struct{ svc *svc.ServiceContext }

func NewAdminAuthMiddleware(s *svc.ServiceContext) *AdminAuthMiddleware {
	return &AdminAuthMiddleware{svc: s}
}
func (m *AdminAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return backofficeAuth(m.svc, backoffice.Admin, next)
}
