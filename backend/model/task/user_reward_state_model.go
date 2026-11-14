package task

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ UserRewardStateModel = (*customUserRewardStateModel)(nil)

type (
	// UserRewardStateModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUserRewardStateModel.
	UserRewardStateModel interface {
		userRewardStateModel
		withSession(session sqlx.Session) UserRewardStateModel
	}

	customUserRewardStateModel struct {
		*defaultUserRewardStateModel
	}
)

// NewUserRewardStateModel returns a model for the database table.
func NewUserRewardStateModel(conn sqlx.SqlConn) UserRewardStateModel {
	return &customUserRewardStateModel{
		defaultUserRewardStateModel: newUserRewardStateModel(conn),
	}
}

func (m *customUserRewardStateModel) withSession(session sqlx.Session) UserRewardStateModel {
	return NewUserRewardStateModel(sqlx.NewSqlConnFromSession(session))
}
