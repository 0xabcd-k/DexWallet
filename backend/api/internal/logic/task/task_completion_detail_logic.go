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

type TaskCompletionDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskCompletionDetailLogic(ctx context.Context, s *svc.ServiceContext) *TaskCompletionDetailLogic {
	return &TaskCompletionDetailLogic{ctx, s}
}
func (l *TaskCompletionDetailLogic) TaskCompletionDetail(req *types.TaskRequest) (*types.TaskCompletionResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TaskCompletion, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	result, err := l.svcCtx.Tasks.Completion(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.CompletionData(l.svcCtx.Config.IDSecret, result)
	if err != nil {
		return nil, err
	}
	return &types.TaskCompletionResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
