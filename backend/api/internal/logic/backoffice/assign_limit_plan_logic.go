// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
)

type AssignLimitPlanLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssignLimitPlanLogic(ctx context.Context, s *svc.ServiceContext) *AssignLimitPlanLogic {
	return &AssignLimitPlanLogic{ctx, s}
}
func (l *AssignLimitPlanLogic) AssignLimitPlan(req *types.AssignLimitPlanRequest) (*types.TransferPolicyResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.User, req.Idx)
	if err != nil {
		return nil, err
	}
	plan, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TransferLimitPlan, req.PlanIdx)
	if err != nil {
		return nil, err
	}
	expected, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.TransferLimitPlan, req.ExpectedPlanIdx)
	if err != nil {
		return nil, err
	}
	p, err := l.svcCtx.Backoffice.AssignLimitPlan(l.ctx, a.Actor, id, plan, expected, req.Reason)
	return httpapi.PolicyReply(l.ctx, l.svcCtx.Config.IDSecret, p, err)

}
