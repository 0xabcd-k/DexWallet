// Code scaffolded by goctl. Safe to edit.
package rewardtransfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type RewardTransferByRequestLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferByRequestLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferByRequestLogic {
	return &RewardTransferByRequestLogic{ctx, s}
}
func (l *RewardTransferByRequestLogic) RewardTransferByRequest(req *types.TaskClaimByRequest) (*types.RewardTransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	r, err := l.svcCtx.RewardTransfers.ByRequest(l.ctx, a.Actor, req.RequestKey)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.RewardTransferData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
