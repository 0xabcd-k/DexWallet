// Code scaffolded by goctl. Safe to edit.
package rewardtransfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type RewardTransferDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferDetailLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferDetailLogic {
	return &RewardTransferDetailLogic{ctx, s}
}
func (l *RewardTransferDetailLogic) RewardTransferDetail(req *types.TaskRequest) (*types.RewardTransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.RewardTransfer, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	r, err := l.svcCtx.RewardTransfers.Detail(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.RewardTransferData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
