package task

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TaskCompletionModel = (*customTaskCompletionModel)(nil)

type (
	// TaskCompletionModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskCompletionModel.
	TaskCompletionModel interface {
		taskCompletionModel
		WithSession(session sqlx.Session) TaskCompletionModel
	}

	customTaskCompletionModel struct {
		*defaultTaskCompletionModel
	}
)

// NewTaskCompletionModel returns a model for the database table.
func NewTaskCompletionModel(conn sqlx.SqlConn) TaskCompletionModel {
	return &customTaskCompletionModel{
		defaultTaskCompletionModel: newTaskCompletionModel(conn),
	}
}

func (m *customTaskCompletionModel) WithSession(session sqlx.Session) TaskCompletionModel {
	return NewTaskCompletionModel(sqlx.NewSqlConnFromSession(session))
}
