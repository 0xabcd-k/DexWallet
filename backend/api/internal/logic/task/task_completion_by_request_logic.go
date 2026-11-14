// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type TaskCompletionByRequestLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskCompletionByRequestLogic(ctx context.Context, s *svc.ServiceContext) *TaskCompletionByRequestLogic {
	return &TaskCompletionByRequestLogic{ctx, s}
}
func (l *TaskCompletionByRequestLogic) TaskCompletionByRequest(req *types.TaskClaimByRequest) (*types.TaskCompletionResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Tasks.CompletionByRequest(l.ctx, a.Actor, req.RequestKey)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.CompletionData(l.svcCtx.Config.IDSecret, result)
	if err != nil {
		return nil, err
	}
	return &types.TaskCompletionResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
