// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
)

type RewardTransferQueueLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferQueueLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferQueueLogic {
	return &RewardTransferQueueLogic{ctx, s}
}
func (l *RewardTransferQueueLogic) RewardTransferQueue(req *types.RewardTransferAdminListRequest) (*types.RewardTransferListResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.RewardTransfer, req.Before)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	rows, next, err := l.svcCtx.Backoffice.RewardTransferQueue(l.ctx, a.Actor, l.svcCtx.RewardTransfers, req.Status, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.RewardTransferListData(l.svcCtx.Config.IDSecret, rows, next)
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferListResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
