package account

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ UserAccountModel = (*customUserAccountModel)(nil)

type (
	// UserAccountModel is an interface to be customized, add more methods here,
	// and implement the added methods in customUserAccountModel.
	UserAccountModel interface {
		userAccountModel
		WithSession(session sqlx.Session) UserAccountModel
		ListByUserID(context.Context, int64) ([]*UserAccount, error)
		PrepareCredit(context.Context, int64, string) error
		FindForUpdate(context.Context, int64, string) (*UserAccount, error)
		SetBalances(context.Context, int64, decimal.Decimal, decimal.Decimal) error
		ListHolds(context.Context, int64, string, int64, int) ([]AccountHold, error)
		ListEntries(context.Context, int64, string, int64, int) ([]StatementEntry, error)
	}

	customUserAccountModel struct {
		*defaultUserAccountModel
	}
)

// NewUserAccountModel returns a model for the database table.
func NewUserAccountModel(conn sqlx.SqlConn) UserAccountModel {
	return &customUserAccountModel{
		defaultUserAccountModel: newUserAccountModel(conn),
	}
}

// WithSession participates in the caller's transaction without committing it.
func (m *customUserAccountModel) WithSession(session sqlx.Session) UserAccountModel {
	return NewUserAccountModel(sqlx.NewSqlConnFromSession(session))
}

func (m *customUserAccountModel) ListByUserID(ctx context.Context, userID int64) ([]*UserAccount, error) {
	rows := make([]*UserAccount, 0)
	err := m.conn.QueryRowsCtx(ctx, &rows, "select "+userAccountRows+" from "+m.table+" where `user_id` = ?", userID)
	return rows, err
}
