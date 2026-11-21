package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BaseReceiveCursorModel = (*customBaseReceiveCursorModel)(nil)

type (
	// BaseReceiveCursorModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBaseReceiveCursorModel.
	BaseReceiveCursorModel interface {
		baseReceiveCursorModel
		withSession(session sqlx.Session) BaseReceiveCursorModel
	}

	customBaseReceiveCursorModel struct {
		*defaultBaseReceiveCursorModel
	}
)

// NewBaseReceiveCursorModel returns a model for the database table.
func NewBaseReceiveCursorModel(conn sqlx.SqlConn) BaseReceiveCursorModel {
	return &customBaseReceiveCursorModel{
		defaultBaseReceiveCursorModel: newBaseReceiveCursorModel(conn),
	}
}

func (m *customBaseReceiveCursorModel) withSession(session sqlx.Session) BaseReceiveCursorModel {
	return NewBaseReceiveCursorModel(sqlx.NewSqlConnFromSession(session))
}
