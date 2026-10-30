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

type BackofficeStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBackofficeStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BackofficeStatusLogic {
	return &BackofficeStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BackofficeStatusLogic) BackofficeStatus(req *types.BackofficeStatusRequest) (resp *types.BackofficeAccountResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	realm := domain.Realm(req.Realm)
	u, err := l.svcCtx.Backoffice.SetStatus(l.ctx, a.Actor, realm, req.Idx, req.Status, req.Reason)
	return accountReply(l.ctx, l.svcCtx.Backoffice, realm, u, err)
}
