// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
	"strconv"
	"time"
)

type SaveTaskDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSaveTaskDraftLogic(ctx context.Context, s *svc.ServiceContext) *SaveTaskDraftLogic {
	return &SaveTaskDraftLogic{ctx, s}
}
func (l *SaveTaskDraftLogic) SaveTaskDraft(req *types.SaveTaskDraftRequest) (*types.ManagedTaskResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	id, err := httpapi.DecodeOptionalIdx(secret, idcodec.Task, req.Idx)
	if err != nil {
		return nil, err
	}
	points, err := strconv.ParseInt(req.BasePoints, 10, 64)
	if err != nil || strconv.FormatInt(points, 10) != req.BasePoints {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	channel, err := strconv.ParseInt(req.ChannelId, 10, 64)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	start, err := time.Parse(time.RFC3339Nano, req.StartsAt)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	end, err := time.Parse(time.RFC3339Nano, req.EndsAt)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	row, err := l.svcCtx.Backoffice.SaveTask(l.ctx, a.Actor, l.svcCtx.Tasks, task.Draft{ID: id, ExpectedRevision: req.ExpectedRevision, RequestKey: req.RequestKey, Title: req.Title, Description: req.Description, BasePoints: points, ChannelID: channel, StartsAt: start, EndsAt: end}, req.Reason)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.ManagedTask(secret, row)
	if err != nil {
		return nil, err
	}

	return &types.ManagedTaskResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
