// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package backoffice

import (
	"context"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/backoffice"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateMerchantLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateMerchantLogic {
	return &CreateMerchantLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateMerchantLogic) CreateMerchant(req *types.BackofficeCreateRequest) (resp *types.BackofficeAccountResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	u, err := l.svcCtx.Backoffice.Create(l.ctx, a.Actor, domain.Merchant, req.Email, req.Password, req.Reason)
	return accountReply(l.ctx, l.svcCtx.Backoffice, domain.Merchant, u, err)
}
