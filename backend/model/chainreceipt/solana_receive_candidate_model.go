package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ SolanaReceiveCandidateModel = (*customSolanaReceiveCandidateModel)(nil)

type (
	// SolanaReceiveCandidateModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolanaReceiveCandidateModel.
	SolanaReceiveCandidateModel interface {
		solanaReceiveCandidateModel
		withSession(session sqlx.Session) SolanaReceiveCandidateModel
	}

	customSolanaReceiveCandidateModel struct {
		*defaultSolanaReceiveCandidateModel
	}
)

// NewSolanaReceiveCandidateModel returns a model for the database table.
func NewSolanaReceiveCandidateModel(conn sqlx.SqlConn) SolanaReceiveCandidateModel {
	return &customSolanaReceiveCandidateModel{
		defaultSolanaReceiveCandidateModel: newSolanaReceiveCandidateModel(conn),
	}
}

func (m *customSolanaReceiveCandidateModel) withSession(session sqlx.Session) SolanaReceiveCandidateModel {
	return NewSolanaReceiveCandidateModel(sqlx.NewSqlConnFromSession(session))
}
