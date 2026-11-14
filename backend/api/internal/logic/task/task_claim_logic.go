// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
)

type TaskClaimLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTaskClaimLogic(ctx context.Context, s *svc.ServiceContext) *TaskClaimLogic {
	return &TaskClaimLogic{ctx, s}
}
func (l *TaskClaimLogic) TaskClaim(req *types.TaskClaimRequest) (*types.TaskCompletionResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	var invitation int64
	if req.InvitationIdx != "" {
		invitation, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TaskInvitation, req.InvitationIdx)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	result, err := l.svcCtx.Tasks.Claim(l.ctx, a.Actor, task.ClaimRequest{TaskID: id, InvitationID: invitation, Key: req.RequestKey, Mode: req.Mode, Stage: req.ExpectedParticipantStage, TaskRevision: req.ExpectedTaskRevision, IdentityRevision: req.ExpectedIdentityRevision})
	if err != nil {
		return nil, err
	}
	data, err := httpapi.CompletionData(l.svcCtx.Config.IDSecret, result)
	if err != nil {
		return nil, err
	}
	return &types.TaskCompletionResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
