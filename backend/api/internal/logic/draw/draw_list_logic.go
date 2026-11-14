// Code scaffolded by goctl. Safe to edit.
package draw

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type DrawListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDrawListLogic(ctx context.Context, s *svc.ServiceContext) *DrawListLogic {
	return &DrawListLogic{ctx, s}
}
func (l *DrawListLogic) DrawList(req *types.DrawListRequest) (*types.DrawListResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.PointDraw, req.Before)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	rows, next, err := l.svcCtx.Draws.List(l.ctx, a.Actor, before, req.Limit)
	if err != nil {
		return nil, err
	}
	data := types.DrawListData{Items: []types.DrawResultData{}}
	for _, row := range rows {
		item, err := httpapi.DrawResultData(l.svcCtx.Config.IDSecret, row)
		if err != nil {
			return nil, err
		}
		data.Items = append(data.Items, item)
	}
	if next != 0 {
		data.NextCursor, err = httpapi.EncodeIdx(l.svcCtx.Config.IDSecret, idcodec.PointDraw, next)
		if err != nil {
			return nil, err
		}
	}
	return &types.DrawListResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
