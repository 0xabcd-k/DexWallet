package deposit

import (
	"context"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"time"
)

var _ DepositQuoteConfirmationModel = (*customDepositQuoteConfirmationModel)(nil)

type (
	// DepositQuoteConfirmationModel is an interface to be customized, add more methods here,
	// and implement the added methods in customDepositQuoteConfirmationModel.
	DepositQuoteConfirmationModel interface {
		depositQuoteConfirmationModel
		ConfirmBeforeExpiry(context.Context, string, int64, string, time.Time, time.Time) error
		ForUser(context.Context, string, int64) (*DepositQuoteConfirmation, error)
		withSession(session sqlx.Session) DepositQuoteConfirmationModel
	}

	customDepositQuoteConfirmationModel struct {
		*defaultDepositQuoteConfirmationModel
	}
)

// NewDepositQuoteConfirmationModel returns a model for the database table.
func NewDepositQuoteConfirmationModel(conn sqlx.SqlConn) DepositQuoteConfirmationModel {
	return &customDepositQuoteConfirmationModel{
		defaultDepositQuoteConfirmationModel: newDepositQuoteConfirmationModel(conn),
	}
}

func (m *customDepositQuoteConfirmationModel) withSession(session sqlx.Session) DepositQuoteConfirmationModel {
	return NewDepositQuoteConfirmationModel(sqlx.NewSqlConnFromSession(session))
}

// The database clock decides admission. Replays never replace the first snapshot.
func (m *customDepositQuoteConfirmationModel) ConfirmBeforeExpiry(ctx context.Context, idx string, user int64, snapshot string, quotedAt, expiry time.Time) error {
	_, err := m.conn.ExecCtx(ctx, `INSERT INTO deposit_quote_confirmation (quote_idx,user_id,snapshot_json,quote_expires_at)
        SELECT ?,?,?,? FROM DUAL WHERE ? <= CURRENT_TIMESTAMP(3) AND CURRENT_TIMESTAMP(3) < ?
        ON DUPLICATE KEY UPDATE quote_idx=deposit_quote_confirmation.quote_idx`, idx, user, snapshot, expiry, quotedAt, expiry)
	return err
}

func (m *customDepositQuoteConfirmationModel) ForUser(ctx context.Context, idx string, user int64) (*DepositQuoteConfirmation, error) {
	var row DepositQuoteConfirmation
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+depositQuoteConfirmationRows+" FROM deposit_quote_confirmation WHERE quote_idx=? AND user_id=?", idx, user)
	return &row, err
}
