package account

import "github.com/zeromicro/go-zero/core/stores/sqlx"

var _ LedgerEntryModel = (*customLedgerEntryModel)(nil)

type (
	// LedgerEntryModel is an interface to be customized, add more methods here,
	// and implement the added methods in customLedgerEntryModel.
	LedgerEntryModel interface {
		ledgerEntryModel
		WithSession(session sqlx.Session) LedgerEntryModel
	}

	customLedgerEntryModel struct {
		*defaultLedgerEntryModel
	}
)

// NewLedgerEntryModel returns a model for the database table.
func NewLedgerEntryModel(conn sqlx.SqlConn) LedgerEntryModel {
	return &customLedgerEntryModel{
		defaultLedgerEntryModel: newLedgerEntryModel(conn),
	}
}

func (m *customLedgerEntryModel) WithSession(session sqlx.Session) LedgerEntryModel {
	return NewLedgerEntryModel(sqlx.NewSqlConnFromSession(session))
}
