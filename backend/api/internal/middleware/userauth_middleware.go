// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package middleware

import (
	"errors"
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
)

type UserAuthMiddleware struct{ svc *svc.ServiceContext }

func NewUserAuthMiddleware(s *svc.ServiceContext) *UserAuthMiddleware {
	return &UserAuthMiddleware{svc: s}
}
func (m *UserAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := verifyActor(m.svc, r)
		if err != nil {
			httpapi.Error(r.Context(), w, err)
			return
		}
		u, err := m.svc.Users.FindUserByID(r.Context(), a.Actor.UserID)
		if errors.Is(err, usermodel.ErrNotFound) {
			err = session.ErrInvalid
		}
		if err == nil && u.Status != usermodel.StatusActive {
			err = userlogic.ErrDisabled
		}
		if err == nil && u.AuthVersion != a.Actor.AuthVersion {
			err = userlogic.ErrStaleSession
		}
		if err == nil {
			a.Current = u
			a.Renewal, err = m.svc.Tokens.Renewal(a.Claims, a.Actor.UserID)
		}
		if err != nil {
			httpapi.Error(r.Context(), w, err)
			return
		}
		next(w, r.WithContext(httpapi.WithAuthorization(r.Context(), a)))
	}
}
