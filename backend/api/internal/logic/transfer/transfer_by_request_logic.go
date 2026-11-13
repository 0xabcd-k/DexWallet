// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type TransferByRequestLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTransferByRequestLogic(ctx context.Context, s *svc.ServiceContext) *TransferByRequestLogic {
	return &TransferByRequestLogic{ctx, s}
}
func (l *TransferByRequestLogic) TransferByRequest(req *types.TransferByRequestRequest) (*types.TransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	row, err := l.svcCtx.Transfers.FindByRequest(l.ctx, a.Actor, req.RequestKey)
	return httpapi.TransferReply(l.ctx, l.svcCtx.Config.IDSecret, a.Actor.UserID, row, err)
}
