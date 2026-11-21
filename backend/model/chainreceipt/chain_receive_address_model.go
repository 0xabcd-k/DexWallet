package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ChainReceiveAddressModel = (*customChainReceiveAddressModel)(nil)

type (
	// ChainReceiveAddressModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainReceiveAddressModel.
	ChainReceiveAddressModel interface {
		chainReceiveAddressModel
		withSession(session sqlx.Session) ChainReceiveAddressModel
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
