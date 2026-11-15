// Code scaffolded by goctl. Safe to edit.
package task

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/task"
)

type InvitationEarningsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewInvitationEarningsLogic(ctx context.Context, s *svc.ServiceContext) *InvitationEarningsLogic {
	return &InvitationEarningsLogic{ctx, s}
}
func (l *InvitationEarningsLogic) InvitationEarnings(req *types.InvitationEarningsRequest) (*types.InvitationEarningsResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	before, err := httpapi.DecodeOptionalIdx(secret, idcodec.RewardGrant, req.Before)
	if err != nil {
		return nil, err
	}
	friend, err := httpapi.DecodeOptionalIdx(secret, idcodec.User, req.FriendIdx)
	if err != nil {
		return nil, err
	}
	rows, next, err := l.svcCtx.Tasks.Earnings(l.ctx, a.Actor, task.EarningsFilter{Before: before, Limit: req.Limit, FriendID: friend, Reason: req.Reason, From: req.From, To: req.To})
	if err != nil {
		return nil, err
	}
	data := types.InvitationEarningsData{Items: []types.InvitationEarningData{}}
	data.NextCursor, err = httpapi.OptionalIdx(secret, idcodec.RewardGrant, next)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		row := types.InvitationEarningData{PeerNickname: r.PeerNickname, Reason: r.Reason, Points: r.Points.StringFixed(0), TaskTitle: r.TaskTitle, CreatedAt: httpapi.TimeString(r.CreatedAt)}
		row.GrantIdx, err = httpapi.EncodeIdx(secret, idcodec.RewardGrant, r.ID)
		if err != nil {
			return nil, err
		}
		row.PeerIdx, err = httpapi.EncodeIdx(secret, idcodec.User, r.PeerID)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, row)
	}

	return &types.InvitationEarningsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
