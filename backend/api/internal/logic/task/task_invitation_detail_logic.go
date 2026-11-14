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

type TaskInvitationDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskInvitationDetailLogic(ctx context.Context, s *svc.ServiceContext) *TaskInvitationDetailLogic {
	return &TaskInvitationDetailLogic{ctx, s}
}
func (l *TaskInvitationDetailLogic) TaskInvitationDetail(req *types.TaskRequest) (*types.TaskInvitationResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TaskInvitation, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	inv, u, err := l.svcCtx.Tasks.Invitation(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.InvitationData(l.svcCtx.Config.IDSecret, inv, u, l.svcCtx.Config.Transfer.WebURL, l.svcCtx.Config.Transfer.TelegramURL)
	if err != nil {
		return nil, err
	}
	return &types.TaskInvitationResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
