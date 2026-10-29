// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package identity

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnbindLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnbindLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbindLogic {
	return &UnbindLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbindLogic) Unbind(req *types.UnbindRequest) (resp *types.TokenResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Auth.Unbind(l.ctx, req.RequestToken, a.Actor, usermodel.IdentityType(req.Identity))
	return httpapi.TokenReply(l.ctx, l.svcCtx.Tokens, result, err)
}
