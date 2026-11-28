// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetTronDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTronDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTronDepositLogic {
	return &GetTronDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTronDepositLogic) GetTronDeposit(req *types.DepositQuoteLookupRequest) (resp *types.TronDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.TronDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.TronDepositOrders.Get(l.ctx, auth.Actor.UserID, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return tronOrderResponse(l.ctx, order), nil
}

func tronOrderResponse(ctx context.Context, o *model.TronDepositOrder) *types.TronDepositOrderResponse {
	data := types.TronDepositOrderData{QuoteIdx: o.QuoteIdx, AssetId: o.AssetId, ActualEnergy: o.ActualEnergy.Int64, State: o.State, TxId: o.TxId, ActualBandwidth: o.ActualBandwidth.Int64}
	if o.ActualFeeSun.Valid {
		data.ActualFeeSun = strconv.FormatInt(o.ActualFeeSun.Int64, 10)
	}
	if o.CreditedUsd.Valid {
		data.CreditedUsd = o.CreditedUsd.Decimal.StringFixed(2)
	}
	return &types.TronDepositOrderResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: data}
}
