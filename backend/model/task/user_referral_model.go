package task

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ UserReferralModel = (*customUserReferralModel)(nil)

type (
	// UserReferralModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUserReferralModel.
	UserReferralModel interface {
		userReferralModel
		withSession(session sqlx.Session) UserReferralModel
	}

	customUserReferralModel struct {
		*defaultUserReferralModel
	}
)

// NewUserReferralModel returns a model for the database table.
func NewUserReferralModel(conn sqlx.SqlConn) UserReferralModel {
	return &customUserReferralModel{
		defaultUserReferralModel: newUserReferralModel(conn),
	}
}

func (m *customUserReferralModel) withSession(session sqlx.Session) UserReferralModel {
	return NewUserReferralModel(sqlx.NewSqlConnFromSession(session))
}
