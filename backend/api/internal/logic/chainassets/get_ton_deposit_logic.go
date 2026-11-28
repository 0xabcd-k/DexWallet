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

type GetTonDepositLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTonDepositLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTonDepositLogic {
	return &GetTonDepositLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTonDepositLogic) GetTonDeposit(req *types.DepositQuoteLookupRequest) (resp *types.TonDepositOrderResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.TonDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	order, err := l.svcCtx.TonDepositOrders.Get(l.ctx, auth.Actor.UserID, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return tonOrderResponse(l.ctx, order), nil
}

func tonOrderResponse(ctx context.Context, o *model.TonDepositOrder) *types.TonDepositOrderResponse {
	data := types.TonDepositOrderData{QuoteIdx: o.QuoteIdx, State: o.State, DispatchBodyHash: o.DispatchHash.String, ReceiveTxHash: o.ReceiveHash.String}
	if o.ActualFeeNano.Valid {
		data.ActualFeeNano = strconv.FormatInt(o.ActualFeeNano.Int64, 10)
	}
	if o.JettonExcessNano.Valid {
		data.JettonSenderFeeNano = strconv.FormatInt(o.JettonSenderFeeNano.Int64, 10)
		data.RecoverableFeeNano = strconv.FormatInt(o.ActualFeeNano.Int64-o.JettonSenderFeeNano.Int64, 10)
		data.JettonExcessNano = strconv.FormatInt(o.JettonExcessNano.Int64, 10)
		data.JettonReserveNano = strconv.FormatInt(o.JettonReserveNano.Int64, 10)
		data.RelaySurplusNano = strconv.FormatInt(o.RelaySurplusNano.Int64, 10)
	}
	if o.CreditedUsd.Valid {
		data.CreditedUsd = o.CreditedUsd.Decimal.StringFixed(2)
	}
	return &types.TonDepositOrderResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: data}
}
