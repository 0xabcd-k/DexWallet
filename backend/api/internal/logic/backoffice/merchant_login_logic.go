// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/backoffice"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MerchantLoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMerchantLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MerchantLoginLogic {
	return &MerchantLoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MerchantLoginLogic) MerchantLogin(req *types.BackofficeLoginRequest) (resp *types.TokenResponse, err error) {
	u, err := l.svcCtx.Backoffice.Login(l.ctx, domain.Merchant, req.Email, req.Password, req.Challenge, req.Code, httpapi.Source(l.ctx), httpapi.TraceID(l.ctx))
	return tokenReply(l.ctx, l.svcCtx.Backoffice, domain.Merchant, u, err)
}
