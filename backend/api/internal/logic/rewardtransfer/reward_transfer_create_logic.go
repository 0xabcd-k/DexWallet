// Code scaffolded by goctl. Safe to edit.
package rewardtransfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/rewardtransfer"
)

type RewardTransferCreateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferCreateLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferCreateLogic {
	return &RewardTransferCreateLogic{ctx, s}
}
func (l *RewardTransferCreateLogic) RewardTransferCreate(req *types.RewardTransferRequest) (*types.RewardTransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	r, err := l.svcCtx.RewardTransfers.Submit(l.ctx, a.Actor, rewardtransfer.Request{Key: req.RequestKey, Amount: req.Amount})
	if err != nil {
		return nil, err
	}
	data, err := httpapi.RewardTransferData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
