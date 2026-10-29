// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package identity

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type IdentityTelegramLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIdentityTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IdentityTelegramLogic {
	return &IdentityTelegramLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *IdentityTelegramLogic) IdentityTelegram(req *types.IdentityTelegramRequest) (resp *types.TokenResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Auth.VerifyTelegram(l.ctx, req.RequestToken, req.InitData, httpapi.Source(l.ctx), authlogic.Intent{Purpose: userlogic.Purpose(req.Purpose), Actor: a.Actor})
	return httpapi.TokenReply(l.ctx, l.svcCtx.Tokens, result, err)
}
