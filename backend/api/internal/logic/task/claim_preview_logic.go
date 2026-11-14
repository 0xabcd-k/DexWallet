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

type ClaimPreviewLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewClaimPreviewLogic(ctx context.Context, s *svc.ServiceContext) *ClaimPreviewLogic {
	return &ClaimPreviewLogic{ctx, s}
}
func (l *ClaimPreviewLogic) ClaimPreview(req *types.ClaimPreviewRequest) (*types.ClaimPreviewResponse, error) {
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
	result, err := l.svcCtx.Tasks.Preview(l.ctx, a.Actor, id, invitation)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.ClaimPreviewData(l.svcCtx.Config.IDSecret, result)
	if err != nil {
		return nil, err
	}
	return &types.ClaimPreviewResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
