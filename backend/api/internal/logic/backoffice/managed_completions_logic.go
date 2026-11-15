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

type ManagedCompletionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewManagedCompletionsLogic(ctx context.Context, s *svc.ServiceContext) *ManagedCompletionsLogic {
	return &ManagedCompletionsLogic{ctx, s}
}
func (l *ManagedCompletionsLogic) ManagedCompletions(req *types.ManagedCompletionRequest) (*types.ManagedCompletionsResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	id, err := httpapi.DecodeIdx(secret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, err
	}
	before, err := httpapi.DecodeOptionalIdx(secret, idcodec.TaskCompletion, req.Before)
	if err != nil {
		return nil, err
	}
	rows, next, err := l.svcCtx.Backoffice.ManagedCompletions(l.ctx, a.Actor, l.svcCtx.Tasks, id, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.ManagedCompletionsData{Items: []types.ManagedCompletionData{}}
	data.NextCursor, err = httpapi.OptionalIdx(secret, idcodec.TaskCompletion, next)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		row := types.ManagedCompletionData{TaskTitle: r.TaskTitle, Mode: r.Mode, TaskRevision: r.TaskRevision, CompletedAt: httpapi.TimeString(r.CreatedAt)}
		row.Idx, err = httpapi.EncodeIdx(secret, idcodec.TaskCompletion, r.Id)
		if err != nil {
			return nil, err
		}
		row.UserIdx, err = httpapi.EncodeIdx(secret, idcodec.User, r.UserId)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, row)
	}

	return &types.ManagedCompletionsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
