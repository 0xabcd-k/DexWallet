// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	"github.com/shopspring/decimal"
)

type SaveDrawVersionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSaveDrawVersionLogic(ctx context.Context, s *svc.ServiceContext) *SaveDrawVersionLogic {
	return &SaveDrawVersionLogic{ctx, s}
}
func (l *SaveDrawVersionLogic) SaveDrawVersion(req *types.SaveDrawVersionRequest) (*types.SaveDrawVersionResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	rtp, err := decimal.NewFromString(req.Rtp)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	r := draw.Rules{RTP: rtp, Prizes: []draw.Prize{}}
	for _, p := range req.Prizes {
		amount, err := decimal.NewFromString(p.RewardUsd)
		if err != nil || len(p.Symbols) != 3 {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
		r.Prizes = append(r.Prizes, draw.Prize{Code: p.Code, RewardUSD: amount, Weight: p.Weight, Symbols: [3]int64{p.Symbols[0], p.Symbols[1], p.Symbols[2]}})
	}
	row, err := l.svcCtx.Backoffice.SaveDrawVersion(l.ctx, a.Actor, l.svcCtx.Draws, req.Name, r, req.Reason)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.ManagedVersion(secret, row)
	if err != nil {
		return nil, err
	}

	return &types.SaveDrawVersionResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
