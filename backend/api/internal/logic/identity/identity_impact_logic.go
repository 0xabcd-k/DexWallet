// Code scaffolded by goctl. Safe to edit.
package identity

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"time"
)

type IdentityImpactLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIdentityImpactLogic(ctx context.Context, s *svc.ServiceContext) *IdentityImpactLogic {
	return &IdentityImpactLogic{ctx, s}
}
func (l *IdentityImpactLogic) IdentityImpact(req *types.IdentityImpactRequest) (*types.IdentityImpactResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	result, err := l.svcCtx.Auth.IdentityImpact(l.ctx, a.Actor, user.Purpose(req.Purpose), model.IdentityType(req.Identity), req.Email, req.InitData, httpapi.Source(l.ctx))
	if err != nil {
		return nil, err
	}
	seconds := int64(0)
	if result.Replacement {
		seconds = int64(user.ReplacementRestriction / time.Second)
	}
	return &types.IdentityImpactResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.IdentityImpactData{Revision: result.Revision, Replacement: result.Replacement, RestrictionSeconds: seconds, RestrictedUntil: httpapi.TimeString(result.RestrictedUntil)}}, nil

}
