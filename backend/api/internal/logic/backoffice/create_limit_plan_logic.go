// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	model "github.com/0xabcd-k/DexWallet/backend/model/transfer"
)

type CreateLimitPlanLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateLimitPlanLogic(ctx context.Context, s *svc.ServiceContext) *CreateLimitPlanLogic {
	return &CreateLimitPlanLogic{ctx, s}
}
func (l *CreateLimitPlanLogic) CreateLimitPlan(req *types.CreateLimitPlanRequest) (*types.LimitPlanResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	min, err := transfer.ParseAmount(req.MinAmount)
	if err != nil {
		return nil, err
	}
	max, err := transfer.ParseAmount(req.MaxAmount)
	if err != nil {
		return nil, err
	}
	daily, err := transfer.ParseAmount(req.DailyOutgoingLimit)
	if err != nil {
		return nil, err
	}
	p, err := l.svcCtx.Backoffice.CreateLimitPlan(l.ctx, a.Actor, model.TransferLimitPlan{Name: req.Name, RequestKey: req.RequestKey, MinAmount: min, MaxAmount: max, DailyOutgoingLimit: daily})
	if err != nil {
		return nil, err
	}
	data, err := httpapi.LimitPlanData(l.svcCtx.Config.IDSecret, p)
	if err != nil {
		return nil, err
	}
	return &types.LimitPlanResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil

}
