package middleware

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"net/http"
)

type MerchantAuthMiddleware struct{ svc *svc.ServiceContext }

func NewMerchantAuthMiddleware(s *svc.ServiceContext) *MerchantAuthMiddleware {
	return &MerchantAuthMiddleware{svc: s}
}
func (m *MerchantAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return backofficeAuth(m.svc, backoffice.Merchant, next)
}
