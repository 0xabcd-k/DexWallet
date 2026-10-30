package backoffice

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BackofficeAuditModel = (*customBackofficeAuditModel)(nil)

type (
	// BackofficeAuditModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBackofficeAuditModel.
	BackofficeAuditModel interface {
		backofficeAuditModel
		withSession(session sqlx.Session) BackofficeAuditModel
	}

	customBackofficeAuditModel struct {
		*defaultBackofficeAuditModel
	}
)

// NewBackofficeAuditModel returns a model for the database table.
func NewBackofficeAuditModel(conn sqlx.SqlConn) BackofficeAuditModel {
	return &customBackofficeAuditModel{
		defaultBackofficeAuditModel: newBackofficeAuditModel(conn),
	}
}

func (m *customBackofficeAuditModel) withSession(session sqlx.Session) BackofficeAuditModel {
	return NewBackofficeAuditModel(sqlx.NewSqlConnFromSession(session))
}
