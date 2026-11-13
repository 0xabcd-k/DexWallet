// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/transfer"
)

type SubmitTransferLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubmitTransferLogic(ctx context.Context, s *svc.ServiceContext) *SubmitTransferLogic {
	return &SubmitTransferLogic{ctx, s}
}
func (l *SubmitTransferLogic) SubmitTransfer(req *types.SubmitTransferRequest) (*types.TransferResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	id, err := httpapi.DecodeIdx(l.svcCtx.Config.IDSecret, idcodec.User, req.RecipientIdx)
	if err != nil {
		return nil, err
	}
	amount, err := transfer.ParseAmount(req.Amount)
	if err != nil {
		return nil, err
	}
	row, err := l.svcCtx.Transfers.Submit(l.ctx, a.Actor, transfer.Request{Key: req.RequestKey, RecipientID: id, Amount: amount})
	return httpapi.TransferReply(l.ctx, l.svcCtx.Config.IDSecret, a.Actor.UserID, row, err)

}
