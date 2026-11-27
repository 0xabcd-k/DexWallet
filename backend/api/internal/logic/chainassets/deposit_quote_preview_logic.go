// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"
	"errors"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	assets "github.com/0xabcd-k/DexWallet/backend/logic/chainassets"

	"github.com/zeromicro/go-zero/core/logx"
)

type DepositQuotePreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDepositQuotePreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DepositQuotePreviewLogic {
	return &DepositQuotePreviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DepositQuotePreviewLogic) DepositQuotePreview(req *types.DepositQuoteRequest) (resp *types.DepositQuoteResponse, err error) {
	authorization, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.ChainAssets == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	var q assets.DepositPreview
	if req.WalletAddress == "" {
		q, err = l.svcCtx.ChainAssets.PreviewDepositContext(l.ctx, req.AssetId, req.TargetUsd, time.Now())
	} else {
		q, err = l.svcCtx.ChainAssets.PreviewWalletDeposit(l.ctx, authorization.Actor.UserID, req.AssetId, req.TargetUsd, req.WalletAddress)
	}
	if err != nil {
		return nil, err
	}
	if q.WalletAddress != "" && l.svcCtx.DepositConfirmations != nil {
		q, err = l.svcCtx.DepositConfirmations.Offer(l.ctx, authorization.Actor.UserID, q)
		if err != nil {
			return nil, err
		}
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return nil, apperror.New(apperror.ServiceUnavailable, errors.New("deposit quote expired during preview"))
	}
	return &types.DepositQuoteResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx),
		Data: depositQuoteData(q)}, nil
}

func depositQuoteData(q assets.DepositPreview) types.DepositQuoteData {
	return types.DepositQuoteData{QuoteIdx: q.QuoteIdx, MaxGas: q.MaxGas, AssetId: q.AssetID, TargetUsd: q.TargetUSD, UsdPerCoin: q.USDPerCoin,
		PayQuantity: q.PayQuantity, GasAssetId: q.GasAssetID, EstimatedGas: q.EstimatedGas, GasUsd: q.GasUSD,
		NetUsd: q.NetUSD, RateVersion: q.Version, RateSource: q.RateSource, AsOf: q.AsOf, QuotedAt: q.QuotedAt, ExpiresAt: q.ExpiresAt,
		EmulatedMessageHash: q.EmulatedMessageHash, RelayAttached: q.RelayAttached, RelaySurplus: q.RelaySurplus, JettonExcess: q.JettonExcess,
		DelegatedEnergy: q.DelegatedEnergy, DelegatedBandwidth: q.DelegatedBandwidth, AccountFunding: q.AccountFunding, AccountFundingUsd: q.AccountFundingUSD,
		WalletFeePreview: q.WalletFeePreview, WalletAddress: q.WalletAddress, MessageSha256: q.MessageSHA256, FeeBlockHash: q.FeeBlockHash, CanSubmit: false,
	}
}
