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

type AuthorizeBaseDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuthorizeBaseDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuthorizeBaseDepositLogic {
	return &AuthorizeBaseDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuthorizeBaseDepositLogic) AuthorizeBaseDeposit(req *types.BaseDepositAuthorizeRequest) (resp *types.BaseDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.BaseDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.BaseDepositOrders.Authorize(l.ctx, auth.Actor, req.QuoteIdx, req.UserSignature)
	if err != nil {
		return nil, err
	}
	return baseOrderResponse(l.ctx, order), nil
}
