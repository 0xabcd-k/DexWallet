// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

type ReceiveCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReceiveCodeLogic(ctx context.Context, s *svc.ServiceContext) *ReceiveCodeLogic {
	return &ReceiveCodeLogic{ctx, s}
}
func (l *ReceiveCodeLogic) ReceiveCode() (*types.ReceiveCodeResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	if !l.svcCtx.ReceiveCodes.Available() {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	idx, err := l.svcCtx.UserIdx.Encode(a.Actor.UserID)
	if err != nil {
		return nil, err
	}
	code, err := l.svcCtx.ReceiveCodes.Encode(idx)
	if err != nil {
		return nil, err
	}
	web, bot := l.svcCtx.ReceiveCodes.Links(code)
	return &types.ReceiveCodeResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.ReceiveCodeData{User: types.CurrentUserData{Idx: idx, Nickname: a.Current.Nickname, AvatarUrl: a.Current.AvatarUrl}, Code: code, Url: web, TelegramUrl: bot}}, nil

}
