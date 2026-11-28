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

type BeneficiarySolPaymentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBeneficiarySolPaymentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BeneficiarySolPaymentsLogic {
	return &BeneficiarySolPaymentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BeneficiarySolPaymentsLogic) BeneficiarySolPayments(req *types.BeneficiaryPaymentsRequest) (resp *types.BeneficiaryPaymentsResponse, err error) {
	auth, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.SolDepositOrders == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	rows, next, err := l.svcCtx.SolDepositOrders.BeneficiaryPayments(l.ctx, auth.Actor.UserID, req.Before)
	if err != nil {
		return nil, err
	}
	data := types.BeneficiaryPaymentsData{Items: make([]types.BeneficiaryPayment, 0, len(rows)), NextCursor: next}
	for _, r := range rows {
		data.Items = append(data.Items, types.BeneficiaryPayment{QuoteIdx: r.QuoteIdx, BeneficiaryIdx: r.BeneficiaryIdx, PayQuantity: r.PayQuantity, TargetUsd: r.TargetUSD, AcceptedAt: r.AcceptedAt.UnixMilli()})
	}
	return &types.BeneficiaryPaymentsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
