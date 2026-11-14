// Code scaffolded by goctl. Safe to edit.
package draw

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type DrawStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDrawStateLogic(ctx context.Context, s *svc.ServiceContext) *DrawStateLogic {
	return &DrawStateLogic{ctx, s}
}
func (l *DrawStateLogic) DrawState() (*types.DrawStateResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	state, err := l.svcCtx.Draws.State(l.ctx, a.Actor)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.DrawStateData(l.svcCtx.Config.IDSecret, state)
	if err != nil {
		return nil, err
	}
	return &types.DrawStateResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
