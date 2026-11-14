// Code scaffolded by goctl. Safe to edit.
package draw

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type DrawDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDrawDetailLogic(ctx context.Context, s *svc.ServiceContext) *DrawDetailLogic {
	return &DrawDetailLogic{ctx, s}
}
func (l *DrawDetailLogic) DrawDetail(req *types.TaskRequest) (*types.DrawResultResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.PointDraw, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	r, err := l.svcCtx.Draws.Detail(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.DrawResultData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.DrawResultResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
