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

type BackofficeAuditsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBackofficeAuditsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BackofficeAuditsLogic {
	return &BackofficeAuditsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BackofficeAuditsLogic) BackofficeAudits(req *types.BackofficeAuditRequest) (resp *types.BackofficeAuditResponse, err error) {
	a, err := domain.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	rows, err := l.svcCtx.Backoffice.Audits(l.ctx, a.Actor, req.Page)
	if err != nil {
		return nil, err
	}
	items := []types.BackofficeAuditItem{}
	for _, row := range rows {
		actor, err := l.svcCtx.Backoffice.Idx(domain.Realm(row.ActorRealm), row.ActorId)
		if err != nil {
			return nil, err
		}
		target, err := l.svcCtx.Backoffice.Idx(domain.Realm(row.TargetRealm), row.TargetId)
		if err != nil {
			return nil, err
		}
		items = append(items, types.BackofficeAuditItem{Id: row.Id, ActorRealm: row.ActorRealm, ActorIdx: actor, Action: row.Action, TargetRealm: row.TargetRealm, TargetIdx: target, TargetEmail: row.TargetEmail, BeforeValue: row.BeforeValue, AfterValue: row.AfterValue, Reason: row.Reason, Result: row.Result, TraceId: row.TraceId, CreatedAt: row.CreatedAt.Unix()})
	}
	return &types.BackofficeAuditResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.BackofficeAuditData{Items: items}}, nil
}
