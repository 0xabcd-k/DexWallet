package deposit

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ SolDepositOrderModel = (*customSolDepositOrderModel)(nil)

type (
	// SolDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolDepositOrderModel.
	SolDepositOrderModel interface {
		solDepositOrderModel
		withSession(session sqlx.Session) SolDepositOrderModel
	}

	customSolDepositOrderModel struct {
		*defaultSolDepositOrderModel
	}
)

// NewSolDepositOrderModel returns a model for the database table.
func NewSolDepositOrderModel(conn sqlx.SqlConn) SolDepositOrderModel {
	return &customSolDepositOrderModel{
		defaultSolDepositOrderModel: newSolDepositOrderModel(conn),
	}
}

func (m *customSolDepositOrderModel) withSession(session sqlx.Session) SolDepositOrderModel {
	return NewSolDepositOrderModel(sqlx.NewSqlConnFromSession(session))
}
