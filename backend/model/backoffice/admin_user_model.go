package backoffice

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ AdminUserModel = (*customAdminUserModel)(nil)

type (
	// AdminUserModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAdminUserModel.
	AdminUserModel interface {
		adminUserModel
		withSession(session sqlx.Session) AdminUserModel
	}

	customAdminUserModel struct {
		*defaultAdminUserModel
	}
)

// NewAdminUserModel returns a model for the database table.
func NewAdminUserModel(conn sqlx.SqlConn) AdminUserModel {
	return &customAdminUserModel{
		defaultAdminUserModel: newAdminUserModel(conn),
	}
}

func (m *customAdminUserModel) withSession(session sqlx.Session) AdminUserModel {
	return NewAdminUserModel(sqlx.NewSqlConnFromSession(session))
}
