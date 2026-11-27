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

type PrepareSolDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPrepareSolDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PrepareSolDepositLogic {
	return &PrepareSolDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PrepareSolDepositLogic) PrepareSolDeposit(req *types.DepositQuoteConfirmRequest) (resp *types.SolDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.SolDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.SolDepositOrders.Prepare(l.ctx, auth.Actor, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return solOrderResponse(l.ctx, order), nil
}
