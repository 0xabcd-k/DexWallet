package rewardtransfer

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ RewardTransferModel = (*customRewardTransferModel)(nil)

type (
	// RewardTransferModel is an interface to be customized, add more methods here,
	// and implement the added methods in customRewardTransferModel.
	RewardTransferModel interface {
		rewardTransferModel
		withSession(session sqlx.Session) RewardTransferModel
	}

	customRewardTransferModel struct {
		*defaultRewardTransferModel
	}
)

// NewRewardTransferModel returns a model for the database table.
func NewRewardTransferModel(conn sqlx.SqlConn) RewardTransferModel {
	return &customRewardTransferModel{
		defaultRewardTransferModel: newRewardTransferModel(conn),
	}
}

func (m *customRewardTransferModel) withSession(session sqlx.Session) RewardTransferModel {
	return NewRewardTransferModel(sqlx.NewSqlConnFromSession(session))
}
