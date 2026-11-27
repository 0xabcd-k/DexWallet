// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSolDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetSolDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSolDepositLogic {
	return &GetSolDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSolDepositLogic) GetSolDeposit(req *types.DepositQuoteLookupRequest) (resp *types.SolDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.SolDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.SolDepositOrders.Get(l.ctx, auth.Actor.UserID, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return solOrderResponse(l.ctx, order), nil
}

func solOrderResponse(ctx context.Context, o deposit.SolOrder) *types.SolDepositOrderResponse {
	data := types.SolDepositOrderData{QuoteIdx: o.QuoteIdx, OrderIdx: o.OrderIdx, State: deposit.AwaitingSignature, MessageBase64: o.MessageBase64, MessageSha256: o.MessageSha256, LastValidBlockHeight: o.LastValidHeight, QuoteExpiresAt: o.QuoteExpiresAt, ReviewAfter: o.ReviewAfter.UnixMilli()}
	if o.AcceptedAt.Valid {
		data.State = deposit.Accepted
		data.AcceptedAt = o.AcceptedAt.Time.UnixMilli()
	}
	return &types.SolDepositOrderResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: data}
}
