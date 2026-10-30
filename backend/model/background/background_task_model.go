package background

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BackgroundTaskModel = (*customBackgroundTaskModel)(nil)

type (
	// BackgroundTaskModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBackgroundTaskModel.
	BackgroundTaskModel interface {
		backgroundTaskModel
		withSession(session sqlx.Session) BackgroundTaskModel
	}

	customBackgroundTaskModel struct {
		*defaultBackgroundTaskModel
	}
)

// NewBackgroundTaskModel returns a model for the database table.
func NewBackgroundTaskModel(conn sqlx.SqlConn) BackgroundTaskModel {
	return &customBackgroundTaskModel{
		defaultBackgroundTaskModel: newBackgroundTaskModel(conn),
	}
}

func (m *customBackgroundTaskModel) withSession(session sqlx.Session) BackgroundTaskModel {
	return NewBackgroundTaskModel(sqlx.NewSqlConnFromSession(session))
}
