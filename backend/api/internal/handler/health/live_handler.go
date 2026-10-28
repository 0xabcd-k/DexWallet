// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package health

import (
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/logic/health"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
)

func LiveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := health.NewLiveLogic(r.Context(), svcCtx)
		resp, err := l.Live()
		if err != nil {
			httpapi.Error(r.Context(), w, err)
		} else {
			httpapi.JSON(r.Context(), w, 200, resp)
		}
	}
}
