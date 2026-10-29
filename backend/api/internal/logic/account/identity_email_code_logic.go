// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type IdentityEmailCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewIdentityEmailCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IdentityEmailCodeLogic {
	return &IdentityEmailCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *IdentityEmailCodeLogic) IdentityEmailCode(req *types.IdentityEmailCodeRequest) (resp *types.ChallengeResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Auth.SendEmail(l.ctx, req.Email, httpapi.Source(l.ctx), req.Locale, authlogic.Intent{Purpose: userlogic.Purpose(req.Purpose), Actor: a.Actor})
	return httpapi.ChallengeReply(l.ctx, result, err)
}
