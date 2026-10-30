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

type MerchantCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMerchantCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MerchantCodeLogic {
	return &MerchantCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MerchantCodeLogic) MerchantCode(req *types.BackofficeCodeRequest) (resp *types.ChallengeResponse, err error) {
	result, err := l.svcCtx.Backoffice.SendCode(l.ctx, domain.Merchant, req.Email, req.Password, httpapi.Source(l.ctx), req.Locale, httpapi.TraceID(l.ctx))
	return httpapi.ChallengeReply(l.ctx, result, err)
}
