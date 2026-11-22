package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TronReceiveCursorModel = (*customTronReceiveCursorModel)(nil)

type (
	// TronReceiveCursorModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTronReceiveCursorModel.
	TronReceiveCursorModel interface {
		tronReceiveCursorModel
		withSession(session sqlx.Session) TronReceiveCursorModel
	}

	customTronReceiveCursorModel struct {
		*defaultTronReceiveCursorModel
	}
)

// NewTronReceiveCursorModel returns a model for the database table.
func NewTronReceiveCursorModel(conn sqlx.SqlConn) TronReceiveCursorModel {
	return &customTronReceiveCursorModel{
		defaultTronReceiveCursorModel: newTronReceiveCursorModel(conn),
	}
}

func (m *customTronReceiveCursorModel) withSession(session sqlx.Session) TronReceiveCursorModel {
	return NewTronReceiveCursorModel(sqlx.NewSqlConnFromSession(session))
}
