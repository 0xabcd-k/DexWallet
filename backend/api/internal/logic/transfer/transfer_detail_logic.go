// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
)

type TransferDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTransferDetailLogic(ctx context.Context, s *svc.ServiceContext) *TransferDetailLogic {
	return &TransferDetailLogic{ctx, s}
}
func (l *TransferDetailLogic) TransferDetail(req *types.TransferDetailRequest) (*types.TransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.Transfer, req.Idx)
	if err != nil {
		return nil, apperror.New(apperror.NotFound, err)
	}
	row, err := l.svcCtx.Transfers.Detail(l.ctx, a.Actor, id)
	return httpapi.TransferReply(l.ctx, l.svcCtx.Config.IDSecret, a.Actor.UserID, row, err)

}
