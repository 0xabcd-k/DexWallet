package chainreceipt

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ BaseReceiveCandidateModel = (*customBaseReceiveCandidateModel)(nil)

type (
	// BaseReceiveCandidateModel is an interface to be customized, add more methods here,
	// and implement the added methods in customBaseReceiveCandidateModel.
	BaseReceiveCandidateModel interface {
		baseReceiveCandidateModel
		withSession(session sqlx.Session) BaseReceiveCandidateModel
	}

	customBaseReceiveCandidateModel struct {
		*defaultBaseReceiveCandidateModel
	}
)

// NewBaseReceiveCandidateModel returns a model for the database table.
func NewBaseReceiveCandidateModel(conn sqlx.SqlConn) BaseReceiveCandidateModel {
	return &customBaseReceiveCandidateModel{
		defaultBaseReceiveCandidateModel: newBaseReceiveCandidateModel(conn),
	}
}

func (m *customBaseReceiveCandidateModel) withSession(session sqlx.Session) BaseReceiveCandidateModel {
	return NewBaseReceiveCandidateModel(sqlx.NewSqlConnFromSession(session))
}
