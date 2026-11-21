package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TonReceiveCursorModel = (*customTonReceiveCursorModel)(nil)

type (
	// TonReceiveCursorModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTonReceiveCursorModel.
	TonReceiveCursorModel interface {
		tonReceiveCursorModel
		withSession(session sqlx.Session) TonReceiveCursorModel
	}

	customTonReceiveCursorModel struct {
		*defaultTonReceiveCursorModel
	}
)

// NewTonReceiveCursorModel returns a model for the database table.
func NewTonReceiveCursorModel(conn sqlx.SqlConn) TonReceiveCursorModel {
	return &customTonReceiveCursorModel{
		defaultTonReceiveCursorModel: newTonReceiveCursorModel(conn),
	}
}

func (m *customTonReceiveCursorModel) withSession(session sqlx.Session) TonReceiveCursorModel {
	return NewTonReceiveCursorModel(sqlx.NewSqlConnFromSession(session))
}
