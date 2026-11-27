// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package chainassets

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/apperror"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DepositQuoteConfirmLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDepositQuoteConfirmLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DepositQuoteConfirmLogic {
	return &DepositQuoteConfirmLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DepositQuoteConfirmLogic) DepositQuoteConfirm(req *types.DepositQuoteConfirmRequest) (resp *types.ConfirmedDepositQuoteResponse, err error) {
	authorization, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.DepositConfirmations == nil {
		return nil, apperror.New(apperror.ServiceUnavailable, nil)
	}
	record, err := l.svcCtx.DepositConfirmations.Confirm(l.ctx, authorization.Actor.UserID, req.QuoteIdx)
	if err != nil {
		return nil, err
	}
	return confirmedQuoteResponse(l.ctx, record), nil
}
