package account

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AccountHoldModel = (*customAccountHoldModel)(nil)

type (
	// AccountHoldModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAccountHoldModel.
	AccountHoldModel interface {
		accountHoldModel
		FindForUpdate(context.Context, int64) (*AccountHold, error)
		Resolve(context.Context, int64, string, string, int64) error
		WithSession(session sqlx.Session) AccountHoldModel
	}

	customAccountHoldModel struct {
		*defaultAccountHoldModel
	}
)

// NewAccountHoldModel returns a model for the database table.
func NewAccountHoldModel(conn sqlx.SqlConn) AccountHoldModel {
	return &customAccountHoldModel{
		defaultAccountHoldModel: newAccountHoldModel(conn),
	}
}

func (m *customAccountHoldModel) WithSession(session sqlx.Session) AccountHoldModel {
	return NewAccountHoldModel(sqlx.NewSqlConnFromSession(session))
}
