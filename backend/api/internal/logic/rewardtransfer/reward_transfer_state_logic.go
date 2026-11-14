// Code scaffolded by goctl. Safe to edit.
package rewardtransfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/rewardtransfer"
	"github.com/shopspring/decimal"
)

type RewardTransferStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferStateLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferStateLogic {
	return &RewardTransferStateLogic{ctx, s}
}
func (l *RewardTransferStateLogic) RewardTransferState() (*types.RewardTransferStateResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	s, err := l.svcCtx.RewardTransfers.State(l.ctx, a.Actor)
	if err != nil {
		return nil, err
	}
	data := types.RewardTransferStateData{Available: s.Available, Pro: s.Pro, MinimumAmount: decimal.NewFromInt(rewardtransfer.MinimumUSD).StringFixed(2), Fee: "0.00", RewardUsd: s.RewardUSD, PendingUsd: s.PendingUSD}
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferStateResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
