package middleware

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"net/http"
	"strings"
)

func backofficeAuth(s *svc.ServiceContext, realm backoffice.Realm, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Backoffice == nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.ServiceUnavailable, nil))
			return
		}
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			httpapi.Error(r.Context(), w, session.ErrInvalid)
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			httpapi.Error(r.Context(), w, session.ErrInvalid)
			return
		}
		a, err := s.Backoffice.Authenticate(r.Context(), realm, parts[1], httpapi.TraceID(r.Context()))
		if err != nil {
			httpapi.Error(r.Context(), w, err)
			return
		}
		base := "/v1/" + string(realm)
		if a.Account.MustChangePassword != 0 && r.URL.Path != base+"/current" && r.URL.Path != base+"/password" {
			httpapi.Error(r.Context(), w, apperror.New(apperror.PermissionDenied, nil))
			return
		}
		ctx := backoffice.WithAuthorization(r.Context(), a)
		ctx = httpapi.WithRenewal(ctx, a.Renewal)
		next(w, r.WithContext(ctx))
	}
}
