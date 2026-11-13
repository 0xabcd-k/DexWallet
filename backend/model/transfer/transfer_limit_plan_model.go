package transfer

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ TransferLimitPlanModel = (*customTransferLimitPlanModel)(nil)

type (
	// TransferLimitPlanModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTransferLimitPlanModel.
	TransferLimitPlanModel interface {
		transferLimitPlanModel
		withSession(session sqlx.Session) TransferLimitPlanModel
	}

	customTransferLimitPlanModel struct {
		*defaultTransferLimitPlanModel
	}
)

// NewTransferLimitPlanModel returns a model for the database table.
func NewTransferLimitPlanModel(conn sqlx.SqlConn) TransferLimitPlanModel {
	return &customTransferLimitPlanModel{
		defaultTransferLimitPlanModel: newTransferLimitPlanModel(conn),
	}
}

func (m *customTransferLimitPlanModel) withSession(session sqlx.Session) TransferLimitPlanModel {
	return NewTransferLimitPlanModel(sqlx.NewSqlConnFromSession(session))
}
