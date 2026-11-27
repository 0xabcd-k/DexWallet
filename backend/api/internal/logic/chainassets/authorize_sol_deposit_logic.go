// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/apperror"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuthorizeSolDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuthorizeSolDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuthorizeSolDepositLogic {
	return &AuthorizeSolDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuthorizeSolDepositLogic) AuthorizeSolDeposit(req *types.SolDepositAuthorizeRequest) (resp *types.SolDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.SolDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.SolDepositOrders.Authorize(l.ctx, auth.Actor, req.QuoteIdx, req.UserSignature)
	if err != nil {
		return nil, err
	}
	return solOrderResponse(l.ctx, order), nil
}
