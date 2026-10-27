// Code scaffolded by goctl. Safe to edit.
package health

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type LiveLogic struct{ ctx context.Context }

func NewLiveLogic(ctx context.Context, _ *svc.ServiceContext) *LiveLogic { return &LiveLogic{ctx: ctx} }
func (l *LiveLogic) Live() (*types.HealthResponse, error) {
	return &types.HealthResponse{Code: httpapi.SuccessCode, Data: types.HealthData{Status: "live"}, TraceId: httpapi.TraceID(l.ctx)}, nil
}
