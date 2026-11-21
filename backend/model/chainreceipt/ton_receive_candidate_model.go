package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TonReceiveCandidateModel = (*customTonReceiveCandidateModel)(nil)

type (
	// TonReceiveCandidateModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTonReceiveCandidateModel.
	TonReceiveCandidateModel interface {
		tonReceiveCandidateModel
		withSession(session sqlx.Session) TonReceiveCandidateModel
	}

	customTonReceiveCandidateModel struct {
		*defaultTonReceiveCandidateModel
	}
)

// NewTonReceiveCandidateModel returns a model for the database table.
func NewTonReceiveCandidateModel(conn sqlx.SqlConn) TonReceiveCandidateModel {
	return &customTonReceiveCandidateModel{
		defaultTonReceiveCandidateModel: newTonReceiveCandidateModel(conn),
	}
}

func (m *customTonReceiveCandidateModel) withSession(session sqlx.Session) TonReceiveCandidateModel {
	return NewTonReceiveCandidateModel(sqlx.NewSqlConnFromSession(session))
}
