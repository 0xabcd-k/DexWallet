package deposit

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TonDepositOrderModel = (*customTonDepositOrderModel)(nil)

type (
	// TonDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTonDepositOrderModel.
	TonDepositOrderModel interface {
		tonDepositOrderModel
		withSession(session sqlx.Session) TonDepositOrderModel
	}

	customTonDepositOrderModel struct {
		*defaultTonDepositOrderModel
	}
)

// NewTonDepositOrderModel returns a model for the database table.
func NewTonDepositOrderModel(conn sqlx.SqlConn) TonDepositOrderModel {
	return &customTonDepositOrderModel{
		defaultTonDepositOrderModel: newTonDepositOrderModel(conn),
	}
}

func (m *customTonDepositOrderModel) withSession(session sqlx.Session) TonDepositOrderModel {
	return NewTonDepositOrderModel(sqlx.NewSqlConnFromSession(session))
}
