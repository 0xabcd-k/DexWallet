// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"context"
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

type AccountEntriesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAccountEntriesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccountEntriesLogic {
	return &AccountEntriesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AccountEntriesLogic) AccountEntries(req *types.AccountEntriesRequest) (resp *types.AccountEntriesResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	entryIDs, err := idcodec.New(l.svcCtx.Config.IDSecret, idcodec.LedgerEntry)
	if err != nil {
		return nil, err
	}
	var before int64
	if req.Before != "" {
		before, err = entryIDs.Decode(req.Before)
		if err != nil {
			return nil, apperror.New(apperror.InvalidArgument, err)
		}
	}
	page, err := l.svcCtx.Accounts.Entries(l.ctx, a.Actor.UserID, req.Kind, before, req.Limit)
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
	data := types.AccountEntriesData{Entries: make([]types.AccountEntry, 0, len(page.Entries))}
	for _, row := range page.Entries {
		entry := types.AccountEntry{BusinessType: row.BusinessType, Action: row.Action, Kind: row.Kind, Unit: row.Unit, Direction: accountlogic.DirectionIn, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
		if accountlogic.Side(row.Side) == accountlogic.SideDebit {
			entry.Direction = accountlogic.DirectionOut
		}
		definition, _ := accountmodel.Lookup(row.Kind)
		entry.Amount, entry.BalanceAfter = row.Amount.StringFixed(definition.Scale), row.BalanceAfter.StringFixed(definition.Scale)
		if entry.Idx, err = entryIDs.Encode(row.ID); err != nil {
			return nil, err
		}
		if entry.VoucherIdx, err = encode(idcodec.LedgerVoucher, row.VoucherID); err != nil {
			return nil, err
		}
		sourceKind, _ := accountlogic.BusinessType(row.BusinessType).IDKind()
		if entry.BusinessIdx, err = encode(sourceKind, row.BusinessID); err != nil {
			return nil, err
		}
		data.Entries = append(data.Entries, entry)
	}
	if page.NextID != 0 {
		if data.NextCursor, err = entryIDs.Encode(page.NextID); err != nil {
			return nil, err
		}
	}
	return &types.AccountEntriesResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
