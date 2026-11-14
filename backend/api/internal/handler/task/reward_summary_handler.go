// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package task

import (
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/logic/task"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
)

func RewardSummaryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := task.NewRewardSummaryLogic(r.Context(), svcCtx)
		resp, err := l.RewardSummary()
		if err != nil {
			httpapi.Error(r.Context(), w, err)
		} else {
			httpapi.JSON(r.Context(), w, http.StatusOK, resp)
		}
	}
}
