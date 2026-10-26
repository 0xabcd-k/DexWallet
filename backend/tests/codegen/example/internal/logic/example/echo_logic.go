// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package example

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type EchoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewEchoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EchoLogic {
	return &EchoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *EchoLogic) Echo(req *types.EchoRequest) (resp *types.EchoResponse, err error) {
	// Keep the response's array representation consistent with the generated TS type.
	items := append([]types.SampleItem{}, req.Items...)
	return &types.EchoResponse{
		Message: req.Message, Amount: req.Amount, Note: req.Note, Items: items,
	}, nil
}
