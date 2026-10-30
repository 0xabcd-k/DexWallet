package background

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ EventOutboxModel = (*customEventOutboxModel)(nil)

type (
	// EventOutboxModel is an interface to be customized, add more methods here,
	// and implement the added methods in customEventOutboxModel.
	EventOutboxModel interface {
		eventOutboxModel
		withSession(session sqlx.Session) EventOutboxModel
	}

	customEventOutboxModel struct {
		*defaultEventOutboxModel
	}
)

// NewEventOutboxModel returns a model for the database table.
func NewEventOutboxModel(conn sqlx.SqlConn) EventOutboxModel {
	return &customEventOutboxModel{
		defaultEventOutboxModel: newEventOutboxModel(conn),
	}
}

func (m *customEventOutboxModel) withSession(session sqlx.Session) EventOutboxModel {
	return NewEventOutboxModel(sqlx.NewSqlConnFromSession(session))
}
