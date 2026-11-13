// Code scaffolded by goctl. Safe to edit.
package transfer

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
)

type TransferPolicyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTransferPolicyLogic(ctx context.Context, s *svc.ServiceContext) *TransferPolicyLogic {
	return &TransferPolicyLogic{ctx, s}
}
func (l *TransferPolicyLogic) TransferPolicy() (*types.TransferPolicyResponse, error) {
	a, err := httpapi.Authorized(l.ctx)
	if err != nil {
		return nil, err
	}
	p, err := l.svcCtx.Transfers.Policy(l.ctx, a.Actor)
	return httpapi.PolicyReply(l.ctx, l.svcCtx.Config.IDSecret, p, err)
}
