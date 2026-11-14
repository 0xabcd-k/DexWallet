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

type TaskCompletionListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskCompletionListLogic(ctx context.Context, s *svc.ServiceContext) *TaskCompletionListLogic {
	return &TaskCompletionListLogic{ctx, s}
}
func (l *TaskCompletionListLogic) TaskCompletionList(req *types.CompletionListRequest) (*types.TaskCompletionListResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TaskCompletion, req.Before)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	var taskID int64
	if req.TaskIdx != "" {
		taskID, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, req.TaskIdx)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	rows, next, err := l.svcCtx.Tasks.Completions(l.ctx, a.Actor, taskID, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.TaskCompletionListData{Items: []types.TaskCompletionData{}}
	for _, row := range rows {
		item, err := httpapi.CompletionData(l.svcCtx.Config.IDSecret, row)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, item)
	}
	if next != 0 {
		data.NextCursor, err = httpapi.EncodeIdx(l.svcCtx.Config.IDSecret, idcodec.TaskCompletion, next)
		if err != nil {
			return nil, err
		}
	}
	return &types.TaskCompletionListResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
