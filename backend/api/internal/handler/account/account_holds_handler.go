// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AccountHoldsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AccountHoldsRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
			return
		}

		l := account.NewAccountHoldsLogic(r.Context(), svcCtx)
		resp, err := l.AccountHolds(&req)
		if err != nil {
			httpapi.Error(r.Context(), w, err)
		} else {
			httpapi.JSON(r.Context(), w, http.StatusOK, resp)
		}
	}
}
