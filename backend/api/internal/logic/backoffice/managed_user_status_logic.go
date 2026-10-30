// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package backoffice

import (
	"context"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/backoffice"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ManagedUserStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewManagedUserStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ManagedUserStatusLogic {
	return &ManagedUserStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ManagedUserStatusLogic) ManagedUserStatus(req *types.ManagedUserStatusRequest) (resp *types.ManagedUserResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	u, err := l.svcCtx.Backoffice.SetProductStatus(l.ctx, a.Actor, req.Idx, req.Status, req.Reason)
	return productReply(l.ctx, l.svcCtx.Backoffice, u, err)
}
