// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package example

import (
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/logic/example"
	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func EchoHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.EchoRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := example.NewEchoLogic(r.Context(), svcCtx)
		resp, err := l.Echo(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
