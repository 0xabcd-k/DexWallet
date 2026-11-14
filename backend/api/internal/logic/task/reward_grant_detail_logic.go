// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type RewardGrantDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardGrantDetailLogic(ctx context.Context, s *svc.ServiceContext) *RewardGrantDetailLogic {
	return &RewardGrantDetailLogic{ctx, s}
}
func (l *RewardGrantDetailLogic) RewardGrantDetail(req *types.TaskRequest) (*types.RewardGrantResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.RewardGrant, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	grant, completion, err := l.svcCtx.Tasks.Grant(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.GrantData(l.svcCtx.Config.IDSecret, grant, completion)
	if err != nil {
		return nil, err
	}
	return &types.RewardGrantResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
