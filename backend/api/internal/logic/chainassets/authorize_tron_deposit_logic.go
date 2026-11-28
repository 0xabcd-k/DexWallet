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

type AuthorizeTronDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuthorizeTronDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuthorizeTronDepositLogic {
	return &AuthorizeTronDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuthorizeTronDepositLogic) AuthorizeTronDeposit(req *types.TronDepositAuthorizeRequest) (resp *types.TronDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.TronDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.TronDepositOrders.Authorize(l.ctx, auth.Actor, req.QuoteIdx, req.UserSignature, req.OrderSignature)
	if err != nil {
		return nil, err
	}
	return tronOrderResponse(l.ctx, order), nil
}
