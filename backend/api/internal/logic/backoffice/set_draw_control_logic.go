// Code scaffolded by goctl. Safe to edit.
package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
)

type SetDrawControlLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSetDrawControlLogic(ctx context.Context, s *svc.ServiceContext) *SetDrawControlLogic {
	return &SetDrawControlLogic{ctx, s}
}
func (l *SetDrawControlLogic) SetDrawControl(req *types.SetDrawControlRequest) (*types.ManagedDrawResponse, error) {
	a, err := backoffice.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	secret := l.svcCtx.Config.IDSecret

	id, err := httpapi.DecodeOptionalIdx(secret, idcodec.DrawVersion, req.VersionIdx)
	if err != nil {
		return nil, err
	}
	_, err = l.svcCtx.Backoffice.SetDrawControl(l.ctx, a.Actor, l.svcCtx.Draws, id, req.ExpectedRevision, req.Reason)
	if err != nil {
		return nil, err
	}

	m, err := l.svcCtx.Backoffice.ManagedDraw(l.ctx, a.Actor, l.svcCtx.Draws, 0, 20)
	if err != nil {
		return nil, err
	}
	data, err := httpapi.ManagedDraw(secret, m, l.svcCtx.Draws.Writable())
	if err != nil {
		return nil, err
	}

	return &types.ManagedDrawResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: data}, nil
}
