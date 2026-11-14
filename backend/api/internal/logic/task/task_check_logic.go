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

type TaskCheckLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskCheckLogic(ctx context.Context, s *svc.ServiceContext) *TaskCheckLogic {
	return &TaskCheckLogic{ctx, s}
}
func (l *TaskCheckLogic) TaskCheck(req *types.TaskRequest) (*types.TaskCheckResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	result, err := l.svcCtx.Tasks.Check(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.TaskData(l.svcCtx.Config.IDSecret, result.Item)
	if err != nil {
		return nil, err
	}
	return &types.TaskCheckResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.TaskCheckData{Task: data, Outcome: result.Outcome, CheckedAt: httpapi.TimeString(result.CheckedAt)}}, nil
}
