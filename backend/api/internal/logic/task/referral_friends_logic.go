// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type ReferralFriendsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReferralFriendsLogic(ctx context.Context, s *svc.ServiceContext) *ReferralFriendsLogic {
	return &ReferralFriendsLogic{ctx, s}
}
func (l *ReferralFriendsLogic) ReferralFriends(req *types.DrawListRequest) (*types.ReferralFriendsResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	before, err := httpapi.DecodeOptionalIdx(secret, idcodec.User, req.Before)
	if err != nil {
		return nil, err
	}
	rows, next, err := l.svcCtx.Tasks.Friends(l.ctx, a.Actor, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.ReferralFriendsData{Items: []types.ReferralFriendData{}}
	data.NextCursor, err = httpapi.OptionalIdx(secret, idcodec.User, next)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		idx, err := httpapi.EncodeIdx(secret, idcodec.User, r.UserID)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, types.ReferralFriendData{UserIdx: idx, Nickname: r.Nickname, BoundAt: httpapi.TimeString(r.BoundAt)})
	}

	return &types.ReferralFriendsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
