package task

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ RewardGrantModel = (*customRewardGrantModel)(nil)

type (
	// RewardGrantModel is an interface to be customized, add more methods here,
	// and implement the added methods in customRewardGrantModel.
	RewardGrantModel interface {
		rewardGrantModel
		withSession(session sqlx.Session) RewardGrantModel
	}

	customRewardGrantModel struct {
		*defaultRewardGrantModel
	}
)

// NewRewardGrantModel returns a model for the database table.
func NewRewardGrantModel(conn sqlx.SqlConn) RewardGrantModel {
	return &customRewardGrantModel{
		defaultRewardGrantModel: newRewardGrantModel(conn),
	}
}

func (m *customRewardGrantModel) withSession(session sqlx.Session) RewardGrantModel {
	return NewRewardGrantModel(sqlx.NewSqlConnFromSession(session))
}
