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

type ChainAssetCatalogLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChainAssetCatalogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChainAssetCatalogLogic {
	return &ChainAssetCatalogLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ChainAssetCatalogLogic) ChainAssetCatalog() (resp *types.ChainAssetCatalogResponse, err error) {
	if _, err := httpapi.Authorized(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.ChainAssets == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	networks := make([]types.ChainNetwork, 0)
	for _, n := range l.svcCtx.ChainAssets.Catalog() {
		assets := make([]types.ChainAsset, 0, len(n.Assets))
		for _, a := range n.Assets {
			assets = append(assets, types.ChainAsset{Id: a.ID, Symbol: a.Symbol, Contract: a.Contract, Decimals: a.Decimals, Origin: a.Origin})
		}
		networks = append(networks, types.ChainNetwork{Id: n.ID, Family: string(n.Family), Testnet: n.Testnet, Enabled: n.Enabled, Assets: assets})
	}
	return &types.ChainAssetCatalogResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.ChainAssetCatalogData{Networks: networks}}, nil
}
