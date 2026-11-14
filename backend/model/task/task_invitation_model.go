package task

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TaskInvitationModel = (*customTaskInvitationModel)(nil)

type (
	// TaskInvitationModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTaskInvitationModel.
	TaskInvitationModel interface {
		taskInvitationModel
		withSession(session sqlx.Session) TaskInvitationModel
	}

	customTaskInvitationModel struct {
		*defaultTaskInvitationModel
	}
)

// NewTaskInvitationModel returns a model for the database table.
func NewTaskInvitationModel(conn sqlx.SqlConn) TaskInvitationModel {
	return &customTaskInvitationModel{
		defaultTaskInvitationModel: newTaskInvitationModel(conn),
	}
}

func (m *customTaskInvitationModel) withSession(session sqlx.Session) TaskInvitationModel {
	return NewTaskInvitationModel(sqlx.NewSqlConnFromSession(session))
}
