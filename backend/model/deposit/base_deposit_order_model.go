package deposit

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BaseDepositOrderModel = (*customBaseDepositOrderModel)(nil)

type (
	// BaseDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBaseDepositOrderModel.
	BaseDepositOrderModel interface {
		baseDepositOrderModel
		withSession(session sqlx.Session) BaseDepositOrderModel
	}

	customBaseDepositOrderModel struct {
		*defaultBaseDepositOrderModel
	}
)

// NewBaseDepositOrderModel returns a model for the database table.
func NewBaseDepositOrderModel(conn sqlx.SqlConn) BaseDepositOrderModel {
	return &customBaseDepositOrderModel{
		defaultBaseDepositOrderModel: newBaseDepositOrderModel(conn),
	}
}

func (m *customBaseDepositOrderModel) withSession(session sqlx.Session) BaseDepositOrderModel {
	return NewBaseDepositOrderModel(sqlx.NewSqlConnFromSession(session))
}
