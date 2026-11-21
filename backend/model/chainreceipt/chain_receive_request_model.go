package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ChainReceiveRequestModel = (*customChainReceiveRequestModel)(nil)

type (
	// ChainReceiveRequestModel is an interface to be customized, add more methods here,
	// and implement the added methods in customChainReceiveRequestModel.
	ChainReceiveRequestModel interface {
		chainReceiveRequestModel
		withSession(session sqlx.Session) ChainReceiveRequestModel
	}

	customChainReceiveRequestModel struct {
		*defaultChainReceiveRequestModel
	}
)

// NewChainReceiveRequestModel returns a model for the database table.
func NewChainReceiveRequestModel(conn sqlx.SqlConn) ChainReceiveRequestModel {
	return &customChainReceiveRequestModel{
		defaultChainReceiveRequestModel: newChainReceiveRequestModel(conn),
	}
}

func (m *customChainReceiveRequestModel) withSession(session sqlx.Session) ChainReceiveRequestModel {
	return NewChainReceiveRequestModel(sqlx.NewSqlConnFromSession(session))
}
