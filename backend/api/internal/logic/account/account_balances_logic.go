// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package account

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AccountBalancesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAccountBalancesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccountBalancesLogic {
	return &AccountBalancesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AccountBalancesLogic) AccountBalances() (resp *types.AccountBalancesResponse, err error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	balances, err := l.svcCtx.Accounts.List(l.ctx, a.Actor.UserID)
	if err != nil {
		return nil, err
	}
	accounts := make([]types.AccountBalance, 0, len(balances))
	for _, balance := range balances {
		accounts = append(accounts, types.AccountBalance{
			Kind: balance.Kind, Unit: balance.Unit, Balance: balance.Balance,
			HeldBalance: balance.HeldBalance, TotalBalance: balance.TotalBalance,
		})
	}
	return &types.AccountBalancesResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.AccountBalancesData{Accounts: accounts}}, nil
}
