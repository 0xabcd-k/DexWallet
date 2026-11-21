package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ SolanaReceiveCursorModel = (*customSolanaReceiveCursorModel)(nil)

type (
	// SolanaReceiveCursorModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolanaReceiveCursorModel.
	SolanaReceiveCursorModel interface {
		solanaReceiveCursorModel
		withSession(session sqlx.Session) SolanaReceiveCursorModel
	}

	customSolanaReceiveCursorModel struct {
		*defaultSolanaReceiveCursorModel
	}
)

// NewSolanaReceiveCursorModel returns a model for the database table.
func NewSolanaReceiveCursorModel(conn sqlx.SqlConn) SolanaReceiveCursorModel {
	return &customSolanaReceiveCursorModel{
		defaultSolanaReceiveCursorModel: newSolanaReceiveCursorModel(conn),
	}
}

func (m *customSolanaReceiveCursorModel) withSession(session sqlx.Session) SolanaReceiveCursorModel {
	return NewSolanaReceiveCursorModel(sqlx.NewSqlConnFromSession(session))
}
