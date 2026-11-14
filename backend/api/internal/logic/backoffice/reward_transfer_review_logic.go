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

type RewardTransferReviewLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardTransferReviewLogic(ctx context.Context, s *svc.ServiceContext) *RewardTransferReviewLogic {
	return &RewardTransferReviewLogic{ctx, s}
}
func (l *RewardTransferReviewLogic) RewardTransferReview(req *types.RewardTransferReviewRequest) (*types.RewardTransferAdminResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.RewardTransfer, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	r, err := l.svcCtx.Backoffice.ReviewRewardTransfer(l.ctx, a.Actor, l.svcCtx.RewardTransfers, id, req.Decision, req.Reason)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.RewardTransferAdminData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.RewardTransferAdminResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
