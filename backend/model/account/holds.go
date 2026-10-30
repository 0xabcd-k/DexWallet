package account

import (
	"context"
	"fmt"
	"strings"
)

const (
	HoldHeld        = "held"
	HoldConsumed    = "consumed"
	HoldReleased    = "released"
	HoldCompleted   = "completed"
	HoldCancelled   = "cancelled"
	HoldRejected    = "rejected"
	HoldNotExecuted = "not_executed"
)

func ValidReleaseReason(reason string) bool {
	return reason == HoldCancelled || reason == HoldRejected || reason == HoldNotExecuted
}

func (m *customAccountHoldModel) FindForUpdate(ctx context.Context, id int64) (*AccountHold, error) {
	var row AccountHold
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+accountHoldRows+" FROM account_hold WHERE id=? FOR UPDATE", id)
	return &row, err
}

func (m *customAccountHoldModel) Resolve(ctx context.Context, id int64, status, reason string, voucher int64) error {
	result, err := m.conn.ExecCtx(ctx, "UPDATE account_hold SET status=?,reason=?,resolved_voucher_id=?,resolved_at=CURRENT_TIMESTAMP(6) WHERE id=? AND status=?", status, reason, voucher, id, HoldHeld)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("hold %d is no longer held", id)
	}
	return nil
}

func (m *customUserAccountModel) ListHolds(ctx context.Context, userID int64, status string, before int64, limit int) ([]AccountHold, error) {
	fields := "h." + strings.Join(accountHoldFieldNames, ",h.")
	query := "SELECT " + fields + " FROM account_hold h JOIN user_account a ON a.id=h.account_id WHERE a.user_id=?"
	args := []any{userID}
	if status != "" {
		query += " AND h.status=?"
		args = append(args, status)
	}
	if before > 0 {
		query += " AND h.id<?"
		args = append(args, before)
	}
	query += " ORDER BY h.id DESC LIMIT ?"
	args = append(args, limit)
	rows := make([]AccountHold, 0)
	err := m.conn.QueryRowsCtx(ctx, &rows, query, args...)
	return rows, err
}
