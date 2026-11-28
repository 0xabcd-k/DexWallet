// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuthorizeTonDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuthorizeTonDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuthorizeTonDepositLogic {
	return &AuthorizeTonDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuthorizeTonDepositLogic) AuthorizeTonDeposit(req *types.TonDepositAuthorizeRequest) (resp *types.TonDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.TonDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.TonDepositOrders.Authorize(l.ctx, auth.Actor, req.QuoteIdx, req.UserSignature, req.OrderSignature)
	if err != nil {
		return nil, err
	}
	return tonOrderResponse(l.ctx, order), nil
}
