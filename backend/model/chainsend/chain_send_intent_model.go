package chainsend

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ChainSendIntentModel = (*customChainSendIntentModel)(nil)

type (
	// ChainSendIntentModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainSendIntentModel.
	ChainSendIntentModel interface {
		chainSendIntentModel
		withSession(session sqlx.Session) ChainSendIntentModel
	}

	customChainSendIntentModel struct {
		*defaultChainSendIntentModel
	}
)

// NewChainSendIntentModel returns a model for the database table.
func NewChainSendIntentModel(conn sqlx.SqlConn) ChainSendIntentModel {
	return &customChainSendIntentModel{
		defaultChainSendIntentModel: newChainSendIntentModel(conn),
	}
}

func (m *customChainSendIntentModel) withSession(session sqlx.Session) ChainSendIntentModel {
	return NewChainSendIntentModel(sqlx.NewSqlConnFromSession(session))
}
