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

type UserLimitPlanLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserLimitPlanLogic(ctx context.Context, s *svc.ServiceContext) *UserLimitPlanLogic {
	return &UserLimitPlanLogic{ctx, s}
}
func (l *UserLimitPlanLogic) UserLimitPlan(req *types.UserLimitPlanRequest) (*types.TransferPolicyResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.User, req.Idx)
	if err != nil {
		return nil, err
	}
	p, err := l.svcCtx.Backoffice.UserLimitPolicy(l.ctx, a.Actor, id)
	return httpapi.PolicyReply(l.ctx, l.svcCtx.Config.IDSecret, p, err)

}
