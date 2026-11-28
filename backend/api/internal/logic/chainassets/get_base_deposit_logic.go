// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetBaseDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetBaseDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBaseDepositLogic {
	return &GetBaseDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBaseDepositLogic) GetBaseDeposit(req *types.DepositQuoteLookupRequest) (resp *types.BaseDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.BaseDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.BaseDepositOrders.Get(l.ctx, auth.Actor.UserID, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return baseOrderResponse(l.ctx, order), nil
}

func baseOrderResponse(ctx context.Context, o *model.BaseDepositOrder) *types.BaseDepositOrderResponse {
	data := types.BaseDepositOrderData{AssetId: o.AssetId, QuoteIdx: o.QuoteIdx, State: o.State, TxHash: o.TxHash.String, ActualFeeWei: o.ActualFeeWei}
	if o.CreditedUsd.Valid {
		data.CreditedUsd = o.CreditedUsd.Decimal.StringFixed(2)
	}
	return &types.BaseDepositOrderResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: data}
}
