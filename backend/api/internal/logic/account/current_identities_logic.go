// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"context"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type CurrentIdentitiesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCurrentIdentitiesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CurrentIdentitiesLogic {
	return &CurrentIdentitiesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CurrentIdentitiesLogic) CurrentIdentities() (resp *types.IdentitiesResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	identities, err := l.svcCtx.Users.FindIdentitiesForActor(l.ctx, a.Actor)
	if err != nil {
		return nil, err
	}
	result := make([]types.IdentitySummary, 0, 2)
	if e := identities.Email; e != nil {
		result = append(result, types.IdentitySummary{Type: string(model.IdentityEmail), Subject: e.EmailAddress, DisplayName: e.EmailOriginal})
	}
	if t := identities.Telegram; t != nil {
		result = append(result, types.IdentitySummary{Type: string(model.IdentityTelegram), Subject: strconv.FormatInt(t.TelegramUserId, 10), DisplayName: t.FirstName})
	}
	return &types.IdentitiesResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.IdentitiesData{Identities: result}}, nil
}
