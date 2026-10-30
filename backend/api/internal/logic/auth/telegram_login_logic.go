// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package auth

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type TelegramLoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTelegramLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelegramLoginLogic {
	return &TelegramLoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelegramLoginLogic) TelegramLogin(req *types.TelegramLoginRequest) (resp *types.TokenResponse, err error) {
	result, err := l.svcCtx.Auth.VerifyTelegram(l.ctx, req.InitData, httpapi.Source(l.ctx), authlogic.Intent{Purpose: userlogic.Login})
	return httpapi.TokenReply(l.ctx, l.svcCtx.Tokens, result, err)
}
