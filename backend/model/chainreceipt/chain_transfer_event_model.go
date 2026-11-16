package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ChainTransferEventModel = (*customChainTransferEventModel)(nil)

type (
	// ChainTransferEventModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainTransferEventModel.
	ChainTransferEventModel interface {
		chainTransferEventModel
		withSession(session sqlx.Session) ChainTransferEventModel
	}

	customChainTransferEventModel struct {
		*defaultChainTransferEventModel
	}
)

// NewChainTransferEventModel returns a model for the database table.
func NewChainTransferEventModel(conn sqlx.SqlConn) ChainTransferEventModel {
	return &customChainTransferEventModel{
		defaultChainTransferEventModel: newChainTransferEventModel(conn),
	}
}

func (m *customChainTransferEventModel) withSession(session sqlx.Session) ChainTransferEventModel {
	return NewChainTransferEventModel(sqlx.NewSqlConnFromSession(session))
}
