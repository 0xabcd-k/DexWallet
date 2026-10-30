package account

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ LedgerVoucherModel = (*customLedgerVoucherModel)(nil)

type (
	// LedgerVoucherModel is an interface to be customized, add more methods here,
	// and implement the added methods in customLedgerVoucherModel.
	LedgerVoucherModel interface {
		ledgerVoucherModel
		WithSession(session sqlx.Session) LedgerVoucherModel
	}

	customLedgerVoucherModel struct {
		*defaultLedgerVoucherModel
	}
)

// NewLedgerVoucherModel returns a model for the database table.
func NewLedgerVoucherModel(conn sqlx.SqlConn) LedgerVoucherModel {
	return &customLedgerVoucherModel{
		defaultLedgerVoucherModel: newLedgerVoucherModel(conn),
	}
}

func (m *customLedgerVoucherModel) WithSession(session sqlx.Session) LedgerVoucherModel {
	return NewLedgerVoucherModel(sqlx.NewSqlConnFromSession(session))
}
