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

type SetTaskStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSetTaskStatusLogic(ctx context.Context, s *svc.ServiceContext) *SetTaskStatusLogic {
	return &SetTaskStatusLogic{ctx, s}
}
func (l *SetTaskStatusLogic) SetTaskStatus(req *types.TaskStatusRequest) (*types.ManagedTaskResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	id, err := httpapi.DecodeIdx(secret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, err
	}
	row, err := l.svcCtx.Backoffice.SetTaskStatus(l.ctx, a.Actor, l.svcCtx.Tasks, id, req.ExpectedRevision, req.Status, req.Reason)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.ManagedTask(secret, row)
	if err != nil {
		return nil, err
	}

	return &types.ManagedTaskResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
