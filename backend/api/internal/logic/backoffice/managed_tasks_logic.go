// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
)

type ManagedTasksLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewManagedTasksLogic(ctx context.Context, s *svc.ServiceContext) *ManagedTasksLogic {
	return &ManagedTasksLogic{ctx, s}
}
func (l *ManagedTasksLogic) ManagedTasks(req *types.DrawListRequest) (*types.ManagedTasksResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	before, err := httpapi.DecodeOptionalIdx(secret, idcodec.Task, req.Before)
	if err != nil {
		return nil, err
	}
	rows, next, err := l.svcCtx.Backoffice.ManagedTasks(l.ctx, a.Actor, l.svcCtx.Tasks, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.ManagedTasksData{Items: []types.ManagedTaskData{}, Writable: l.svcCtx.Tasks.Writable()}
	data.NextCursor, err = httpapi.OptionalIdx(secret, idcodec.Task, next)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		row, err := httpapi.ManagedTask(secret, r)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, row)
	}

	return &types.ManagedTasksResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
