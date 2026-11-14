// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type TaskListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskListLogic(ctx context.Context, s *svc.ServiceContext) *TaskListLogic {
	return &TaskListLogic{ctx, s}
}
func (l *TaskListLogic) TaskList(req *types.TaskListRequest) (*types.TaskListResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, req.Before)
		if err != nil {
			return nil, err
		}
	}
	page, err := l.svcCtx.Tasks.List(l.ctx, a.Actor, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.TaskListData{Items: []types.TaskData{}}
	for _, item := range page.Items {
		entry, err := httpapi.TaskData(l.svcCtx.Config.IDSecret, item)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, entry)
	}
	if page.NextID > 0 {
		data.NextCursor, err = httpapi.EncodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, page.NextID)
		if err != nil {
			return nil, err
		}
	}
	return &types.TaskListResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
