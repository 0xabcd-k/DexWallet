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

type ChainTransferPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChainTransferPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChainTransferPreviewLogic {
	return &ChainTransferPreviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ChainTransferPreviewLogic) ChainTransferPreview(req *types.ChainTransferPreviewRequest) (resp *types.ChainTransferPreviewResponse, err error) {
	if _, err := httpapi.Authorized(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.ChainAssets == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	result, err := l.svcCtx.ChainAssets.PreviewTransfer(chainassets.TargetInput{Network: req.Network, AssetID: req.AssetId, Address: req.Address}, req.Amount)
	if err != nil {
		return nil, err
	}
	return &types.ChainTransferPreviewResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.ChainTransferPreviewData{Target: targetData(result.Target), Amount: result.Amount, CanSubmit: result.CanSubmit, BlockReason: result.BlockReason}}, nil
}
