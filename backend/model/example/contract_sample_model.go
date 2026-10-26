package example

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ ContractSampleModel = (*customContractSampleModel)(nil)

type (
	// ContractSampleModel is an interface to be customized, add more methods here,
	// and implement the added methods in customContractSampleModel.
	ContractSampleModel interface {
		contractSampleModel
		WithSession(session sqlx.Session) ContractSampleModel
	}

	customContractSampleModel struct {
		*defaultContractSampleModel
	}
)

// NewContractSampleModel returns a model for the database table.
func NewContractSampleModel(conn sqlx.SqlConn) ContractSampleModel {
	return &customContractSampleModel{
		defaultContractSampleModel: newContractSampleModel(conn),
	}
}

// WithSession participates in the caller's transaction without committing it.
func (m *customContractSampleModel) WithSession(session sqlx.Session) ContractSampleModel {
	return NewContractSampleModel(sqlx.NewSqlConnFromSession(session))
}
