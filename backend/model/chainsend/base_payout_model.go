package chainsend

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BasePayoutModel = (*customBasePayoutModel)(nil)

type (
	// BasePayoutModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBasePayoutModel.
	BasePayoutModel interface {
		basePayoutModel
		withSession(session sqlx.Session) BasePayoutModel
	}

	customBasePayoutModel struct {
		*defaultBasePayoutModel
	}
)

// NewBasePayoutModel returns a model for the database table.
func NewBasePayoutModel(conn sqlx.SqlConn) BasePayoutModel {
	return &customBasePayoutModel{
		defaultBasePayoutModel: newBasePayoutModel(conn),
	}
}

func (m *customBasePayoutModel) withSession(session sqlx.Session) BasePayoutModel {
	return NewBasePayoutModel(sqlx.NewSqlConnFromSession(session))
}
