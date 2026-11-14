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

type TaskDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskDetailLogic(ctx context.Context, s *svc.ServiceContext) *TaskDetailLogic {
	return &TaskDetailLogic{ctx, s}
}
func (l *TaskDetailLogic) TaskDetail(req *types.TaskRequest) (*types.TaskResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	item, err := l.svcCtx.Tasks.Detail(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.TaskData(l.svcCtx.Config.IDSecret, item)
	if err != nil {
		return nil, err
	}
	return &types.TaskResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
