package account

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// PrepareCredit leaves an existing balance untouched and takes the same unique
// key lock as concurrent first credits. Callers order all account locks first.
func (m *customUserAccountModel) PrepareCredit(ctx context.Context, userID int64, kind string) error {
	_, err := m.conn.ExecCtx(ctx, "INSERT INTO user_account(user_id,kind) VALUES(?,?) ON DUPLICATE KEY UPDATE id=id", userID, kind)
	return err
}

func (m *customUserAccountModel) FindForUpdate(ctx context.Context, userID int64, kind string) (*UserAccount, error) {
	var row UserAccount
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+userAccountRows+" FROM user_account WHERE user_id=? AND kind=? FOR UPDATE", userID, kind)
	return &row, err
}

func (m *customUserAccountModel) SetBalances(ctx context.Context, id int64, balance, held decimal.Decimal) error {
	_, err := m.conn.ExecCtx(ctx, "UPDATE user_account SET balance=?,held_balance=? WHERE id=?", balance, held, id)
	return err
}

// StatementEntry deliberately omits counterpart accounts and system entries.
type StatementEntry struct {
	ID           int64           `db:"id"`
	VoucherID    int64           `db:"voucher_id"`
	BusinessType string          `db:"business_type"`
	BusinessID   int64           `db:"business_id"`
	Action       string          `db:"action"`
	Kind         string          `db:"kind"`
	Unit         string          `db:"unit"`
	Side         string          `db:"side"`
	Amount       decimal.Decimal `db:"amount"`
	BalanceAfter decimal.Decimal `db:"balance_after"`
	BalancePart  string          `db:"balance_part"`
	CreatedAt    time.Time       `db:"created_at"`
}

func (m *customUserAccountModel) ListEntries(ctx context.Context, userID int64, kind string, before int64, limit int) ([]StatementEntry, error) {
	query := `SELECT e.id,e.voucher_id,v.business_type,v.business_id,v.action,a.kind,e.unit,e.side,e.amount,e.balance_after,e.balance_part,v.created_at
		FROM user_account a JOIN ledger_entry e ON e.account_id=a.id JOIN ledger_voucher v ON v.id=e.voucher_id WHERE a.user_id=? AND NOT(e.balance_part='held' AND v.action IN ('hold','release'))`
	args := []any{userID}
	if kind != "" {
		query += " AND a.kind=?"
		args = append(args, kind)
	}
	if before > 0 {
		query += " AND e.id<?"
		args = append(args, before)
	}
	query += " ORDER BY e.id DESC LIMIT ?"
	args = append(args, limit)
	rows := make([]StatementEntry, 0)
	err := m.conn.QueryRowsCtx(ctx, &rows, query, args...)
	return rows, err
}
