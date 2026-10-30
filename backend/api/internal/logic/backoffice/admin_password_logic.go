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

type AdminPasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdminPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminPasswordLogic {
	return &AdminPasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminPasswordLogic) AdminPassword(req *types.BackofficeChangePasswordRequest) (resp *types.TokenResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	u, err := l.svcCtx.Backoffice.ChangePassword(l.ctx, a.Actor, req.OldPassword, req.Password)
	return tokenReply(l.ctx, l.svcCtx.Backoffice, domain.Admin, u, err)
}
