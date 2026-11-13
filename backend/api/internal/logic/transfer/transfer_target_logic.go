// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

type TransferTargetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTransferTargetLogic(ctx context.Context, s *svc.ServiceContext) *TransferTargetLogic {
	return &TransferTargetLogic{ctx, s}
}
func (l *TransferTargetLogic) TransferTarget(req *types.TransferTargetRequest) (*types.TransferTargetResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}

	if (req.RecipientIdx == "") == (req.ReceiveCode == "") {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	idx := req.RecipientIdx
	if req.ReceiveCode != "" {
		if !l.svcCtx.ReceiveCodes.Available() {
			return nil, apperror.New(apperror.ServiceUnavailable, nil)
		}
		idx, err = l.svcCtx.ReceiveCodes.Resolve(req.ReceiveCode)
		if err != nil {
			return nil, err
		}
	}
	id, err := l.svcCtx.UserIdx.Decode(idx)
	if err != nil {
		return nil, apperror.New(apperror.InvalidArgument, err)
	}
	u, err := l.svcCtx.Transfers.Target(l.ctx, a.Actor, id)
	if err != nil {
		return nil, err
	}
	return &types.TransferTargetResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(l.ctx), Data: types.CurrentUserData{Idx: idx, Nickname: u.Nickname, AvatarUrl: u.AvatarUrl}}, nil

}
