package chainreceipt

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ ChainReceiveAddressModel = (*customChainReceiveAddressModel)(nil)

type (
	// ChainReceiveAddressModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainReceiveAddressModel.
	ChainReceiveAddressModel interface {
		chainReceiveAddressModel
		withSession(session sqlx.Session) ChainReceiveAddressModel
		RegisteredRoute(ctx context.Context, network, owner string) (string, error)
	}

	customChainReceiveAddressModel struct {
		*defaultChainReceiveAddressModel
	}
)

// NewChainReceiveAddressModel returns a model for the database table.
func NewChainReceiveAddressModel(conn sqlx.SqlConn) ChainReceiveAddressModel {
	return &customChainReceiveAddressModel{
		defaultChainReceiveAddressModel: newChainReceiveAddressModel(conn),
	}
}

func (m *customChainReceiveAddressModel) withSession(session sqlx.Session) ChainReceiveAddressModel {
	return NewChainReceiveAddressModel(sqlx.NewSqlConnFromSession(session))
}

// An insert can encounter a concurrent registration newer than the import
// transaction's read view. Read the current route of that unique key.
func (m *customChainReceiveAddressModel) RegisteredRoute(ctx context.Context, network, owner string) (string, error) {
	var route string
	err := m.conn.QueryRowCtx(ctx, &route, "SELECT route FROM chain_receive_address WHERE network=? AND owner=? FOR SHARE", network, owner)
	return route, err
}
