package deposit

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TronDepositOrderModel = (*customTronDepositOrderModel)(nil)

type (
	// TronDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTronDepositOrderModel.
	TronDepositOrderModel interface {
		tronDepositOrderModel
		withSession(session sqlx.Session) TronDepositOrderModel
	}

	customTronDepositOrderModel struct {
		*defaultTronDepositOrderModel
	}
)

// NewTronDepositOrderModel returns a model for the database table.
func NewTronDepositOrderModel(conn sqlx.SqlConn) TronDepositOrderModel {
	return &customTronDepositOrderModel{
		defaultTronDepositOrderModel: newTronDepositOrderModel(conn),
	}
}

func (m *customTronDepositOrderModel) withSession(session sqlx.Session) TronDepositOrderModel {
	return NewTronDepositOrderModel(sqlx.NewSqlConnFromSession(session))
}
