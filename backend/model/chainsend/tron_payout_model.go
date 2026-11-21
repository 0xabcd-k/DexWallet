package chainsend

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TronPayoutModel = (*customTronPayoutModel)(nil)

type (
	// TronPayoutModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTronPayoutModel.
	TronPayoutModel interface {
		tronPayoutModel
		withSession(session sqlx.Session) TronPayoutModel
	}

	customTronPayoutModel struct {
		*defaultTronPayoutModel
	}
)

// NewTronPayoutModel returns a model for the database table.
func NewTronPayoutModel(conn sqlx.SqlConn) TronPayoutModel {
	return &customTronPayoutModel{
		defaultTronPayoutModel: newTronPayoutModel(conn),
	}
}

func (m *customTronPayoutModel) withSession(session sqlx.Session) TronPayoutModel {
	return NewTronPayoutModel(sqlx.NewSqlConnFromSession(session))
}
