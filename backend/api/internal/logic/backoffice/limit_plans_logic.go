// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
)

type LimitPlansLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLimitPlansLogic(ctx context.Context, s *svc.ServiceContext) *LimitPlansLogic {
	return &LimitPlansLogic{ctx, s}
}
func (l *LimitPlansLogic) LimitPlans(req *types.LimitPlansRequest) (*types.LimitPlansResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	rows, err := l.svcCtx.Backoffice.LimitPlans(l.ctx, a.Actor, req.Page)
	if err != nil {
		return nil, err
	}
	plans := make([]types.TransferLimitPlan, 0, len(rows))
	for _, p := range rows {
		v, err := httpapi.LimitPlanData(l.svcCtx.Config.IDSecret, &p)
		if err != nil {
			return nil, err
		}
		plans = append(plans, v)
	}
	return &types.LimitPlansResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.LimitPlansData{Plans: plans}}, nil

}
