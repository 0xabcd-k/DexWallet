// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"context"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	accountlogic "github.com/0xabcd-k/DexWallet/backend/logic/account"
	accountmodel "github.com/0xabcd-k/DexWallet/backend/model/account"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AccountHoldsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAccountHoldsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccountHoldsLogic {
	return &AccountHoldsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AccountHoldsLogic) AccountHolds(req *types.AccountHoldsRequest) (resp *types.AccountHoldsResponse, err error) {
	actor, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	holdIDs, err := idcodec.New(l.svcCtx.Config.IDSecret, idcodec.AccountHold)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = holdIDs.Decode(req.Before)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	page, err := l.svcCtx.Accounts.Holds(l.ctx, actor.Actor.UserID, req.Status, before, req.Limit)
	if err != nil {
		return nil, err
	}
	encode := func(kind idcodec.Kind, id int64) (string, error) {
		codec, err := idcodec.New(l.svcCtx.Config.IDSecret, kind)
		if err != nil {
			return "", err
		}
		return codec.Encode(id)
	}
	data := types.AccountHoldsData{Holds: make([]types.AccountHold, 0, len(page.Holds))}
	for _, row := range page.Holds {
		item := types.AccountHold{BusinessType: row.BusinessType, Amount: row.Amount.StringFixed(2), Unit: accountmodel.UnitUSD,
			Status: row.Status, Reason: row.Reason, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
		if item.Idx, err = holdIDs.Encode(row.Id); err != nil {
			return nil, err
		}
		source, ok := accountlogic.BusinessType(row.BusinessType).IDKind()
		if !ok {
			return nil, fmt.Errorf("stored hold %d has unknown source %q", row.Id, row.BusinessType)
		}
		if item.BusinessIdx, err = encode(source, row.BusinessId); err != nil {
			return nil, err
		}
		if item.HoldVoucherIdx, err = encode(idcodec.LedgerVoucher, row.HoldVoucherId); err != nil {
			return nil, err
		}
		if row.ResolvedVoucherId.Valid {
			if item.ResolvedVoucherIdx, err = encode(idcodec.LedgerVoucher, row.ResolvedVoucherId.Int64); err != nil {
				return nil, err
			}
		}
		if row.ResolvedAt.Valid {
			item.ResolvedAt = row.ResolvedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		data.Holds = append(data.Holds, item)
	}
	if page.NextID != 0 {
		if data.NextCursor, err = holdIDs.Encode(page.NextID); err != nil {
			return nil, err
		}
	}
	return &types.AccountHoldsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
