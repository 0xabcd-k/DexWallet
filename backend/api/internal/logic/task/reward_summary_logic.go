// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type RewardSummaryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRewardSummaryLogic(ctx context.Context, s *svc.ServiceContext) *RewardSummaryLogic {
	return &RewardSummaryLogic{ctx, s}
}
func (l *RewardSummaryLogic) RewardSummary() (*types.RewardSummaryResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Tasks.Summary(l.ctx, a.Actor)
	if err != nil {
		return nil, err
	}
	data := httpapi.RewardSummaryData(result)
	return &types.RewardSummaryResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
