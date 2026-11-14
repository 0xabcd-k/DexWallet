// Code scaffolded by goctl. Safe to edit.
package draw

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type DrawByRequestLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDrawByRequestLogic(ctx context.Context, s *svc.ServiceContext) *DrawByRequestLogic {
	return &DrawByRequestLogic{ctx, s}
}
func (l *DrawByRequestLogic) DrawByRequest(req *types.TaskClaimByRequest) (*types.DrawResultResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	r, err := l.svcCtx.Draws.ByRequest(l.ctx, a.Actor, req.RequestKey)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.DrawResultData(l.svcCtx.Config.IDSecret, r)
	if err != nil {
		return nil, err
	}
	return &types.DrawResultResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
