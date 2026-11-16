// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChainBalancesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChainBalancesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChainBalancesLogic {
	return &ChainBalancesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ChainBalancesLogic) ChainBalances(req *types.ChainBalancesRequest) (resp *types.ChainBalancesResponse, err error) {
	authorization, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.ChainAssets == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	result, err := l.svcCtx.ChainAssets.Query(l.ctx, authorization.Actor.UserID, req.Network, req.Address)
	if err != nil {
		return nil, err
	}
	items := make([]types.ChainAssetBalance, 0, len(result.Items))
	for _, b := range result.Items {
		items = append(items, types.ChainAssetBalance{AssetId: b.AssetID, Quantity: b.Quantity, Status: b.Status, ErrorCode: b.ErrorCode, QueriedAt: b.QueriedAt})
	}
	return &types.ChainBalancesResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.ChainBalancesData{Network: result.Network, Address: result.Address, Items: items}}, nil
}
