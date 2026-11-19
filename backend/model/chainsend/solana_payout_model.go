package chainsend

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ SolanaPayoutModel = (*customSolanaPayoutModel)(nil)

type (
	// SolanaPayoutModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolanaPayoutModel.
	SolanaPayoutModel interface {
		solanaPayoutModel
		withSession(session sqlx.Session) SolanaPayoutModel
	}

	customSolanaPayoutModel struct {
		*defaultSolanaPayoutModel
	}
)

// NewSolanaPayoutModel returns a model for the database table.
func NewSolanaPayoutModel(conn sqlx.SqlConn) SolanaPayoutModel {
	return &customSolanaPayoutModel{
		defaultSolanaPayoutModel: newSolanaPayoutModel(conn),
	}
}

func (m *customSolanaPayoutModel) withSession(session sqlx.Session) SolanaPayoutModel {
	return NewSolanaPayoutModel(sqlx.NewSqlConnFromSession(session))
}
