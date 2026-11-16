// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChainTargetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChainTargetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChainTargetLogic {
	return &ChainTargetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ChainTargetLogic) ChainTarget(req *types.ChainTargetRequest) (resp *types.ChainTargetResponse, err error) {
	if _, err := httpapi.Authorized(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.ChainAssets == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	result, err := l.svcCtx.ChainAssets.ResolveTarget(chainassets.TargetInput{Network: req.Network, AssetID: req.AssetId, Address: req.Address, ReceiveCode: req.ReceiveCode})
	if err != nil {
		return nil, err
	}
	return &types.ChainTargetResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: targetData(result)}, nil
}
