// Code scaffolded by goctl. Safe to edit.
package draw

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
)

type DrawCreateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDrawCreateLogic(ctx context.Context, s *svc.ServiceContext) *DrawCreateLogic {
	return &DrawCreateLogic{ctx, s}
}
func (l *DrawCreateLogic) DrawCreate(req *types.DrawRequest) (*types.DrawResultResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.DrawVersion, req.VersionIdx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	r, err := l.svcCtx.Draws.Draw(l.ctx, a.Actor, draw.Request{Key: req.RequestKey, VersionID: id})
	if err != nil {
		return nil, err
	}
	data, err := httpapi.DrawResultData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.DrawResultResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
