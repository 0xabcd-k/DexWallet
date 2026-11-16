package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ChainObservedTransactionModel = (*customChainObservedTransactionModel)(nil)

type (
	// ChainObservedTransactionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainObservedTransactionModel.
	ChainObservedTransactionModel interface {
		chainObservedTransactionModel
		withSession(session sqlx.Session) ChainObservedTransactionModel
	}

	customChainObservedTransactionModel struct {
		*defaultChainObservedTransactionModel
	}
)

// NewChainObservedTransactionModel returns a model for the database table.
func NewChainObservedTransactionModel(conn sqlx.SqlConn) ChainObservedTransactionModel {
	return &customChainObservedTransactionModel{
		defaultChainObservedTransactionModel: newChainObservedTransactionModel(conn),
	}
}

func (m *customChainObservedTransactionModel) withSession(session sqlx.Session) ChainObservedTransactionModel {
	return NewChainObservedTransactionModel(sqlx.NewSqlConnFromSession(session))
}
