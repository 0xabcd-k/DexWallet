package backoffice

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BackofficeChallengeModel = (*customBackofficeChallengeModel)(nil)

type (
	// BackofficeChallengeModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBackofficeChallengeModel.
	BackofficeChallengeModel interface {
		backofficeChallengeModel
		withSession(session sqlx.Session) BackofficeChallengeModel
	}

	customBackofficeChallengeModel struct {
		*defaultBackofficeChallengeModel
	}
)

// NewBackofficeChallengeModel returns a model for the database table.
func NewBackofficeChallengeModel(conn sqlx.SqlConn) BackofficeChallengeModel {
	return &customBackofficeChallengeModel{
		defaultBackofficeChallengeModel: newBackofficeChallengeModel(conn),
	}
}

func (m *customBackofficeChallengeModel) withSession(session sqlx.Session) BackofficeChallengeModel {
	return NewBackofficeChallengeModel(sqlx.NewSqlConnFromSession(session))
}
