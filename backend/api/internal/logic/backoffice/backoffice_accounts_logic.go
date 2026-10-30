// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/backoffice"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type BackofficeAccountsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBackofficeAccountsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BackofficeAccountsLogic {
	return &BackofficeAccountsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BackofficeAccountsLogic) BackofficeAccounts(req *types.BackofficeListRequest) (resp *types.BackofficeAccountsResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	realm := domain.Realm(req.Realm)
	rows, err := l.svcCtx.Backoffice.List(l.ctx, a.Actor, realm, req.Page)
	if err != nil {
		return nil, err
	}
	items := []types.BackofficeAccount{}
	for i := range rows {
		item, err := accountData(l.svcCtx.Backoffice, realm, &rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return &types.BackofficeAccountsResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.BackofficeAccountsData{Items: items}}, nil
}
