// Code scaffolded by goctl. Safe to edit.
package health

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

type ReadyLogic struct {
	ctx context.Context
	svc *svc.ServiceContext
}

func NewReadyLogic(ctx context.Context, s *svc.ServiceContext) *ReadyLogic {
	return &ReadyLogic{ctx: ctx, svc: s}
}
func (l *ReadyLogic) Ready() (*types.HealthResponse, error) {
	if !l.svc.Health.Ready() {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	return &types.HealthResponse{Code: httpapi.SuccessCode, Data: types.HealthData{Status: "ready"}, TraceId: httpapi.TraceID(l.ctx)}, nil
}
