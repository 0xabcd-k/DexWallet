package deposit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ SolDepositOrderModel = (*customSolDepositOrderModel)(nil)

type (
	// SolDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolDepositOrderModel.
	SolDepositOrderModel interface {
		solDepositOrderModel
		ListBeneficiaryPayments(context.Context, int64, string, int) ([]BeneficiaryPayment, error)
		LockByQuoteIdx(context.Context, string) (*SolDepositOrder, error)
		MarkCredited(context.Context, int64, int64, decimal.Decimal) error
		AdvanceExecution(context.Context, *SolDepositOrder, string, sql.NullString, string, int64, int64, int64) error
		withSession(session sqlx.Session) SolDepositOrderModel
	}

	customSolDepositOrderModel struct {
		*defaultSolDepositOrderModel
	}
)

// NewSolDepositOrderModel returns a model for the database table.
func NewSolDepositOrderModel(conn sqlx.SqlConn) SolDepositOrderModel {
	return &customSolDepositOrderModel{
		defaultSolDepositOrderModel: newSolDepositOrderModel(conn),
	}
}

func (m *customSolDepositOrderModel) withSession(session sqlx.Session) SolDepositOrderModel {
	return NewSolDepositOrderModel(sqlx.NewSqlConnFromSession(session))
}

// Compare the previous state so a delayed RPC observation cannot reopen a
// terminal order or overwrite another worker's review decision.
func (m *customSolDepositOrderModel) AdvanceExecution(ctx context.Context, old *SolDepositOrder, state string, sig sql.NullString, policy string, fee, slot, funding int64) error {
	_, err := m.conn.ExecCtx(ctx, `UPDATE sol_deposit_order SET execution_state=?,tx_signature=?,sponsor_policy=?,actual_lamports_fee=?,finalized_slot=?,actual_funding_lamports=?
 WHERE quote_idx=? AND accepted_at IS NOT NULL AND execution_state=? AND tx_signature <=> ?`, state, sig, policy, fee, slot, funding, old.QuoteIdx, old.ExecutionState, old.TxSignature)
	return err
}

func (m *customSolDepositOrderModel) LockByQuoteIdx(ctx context.Context, idx string) (*SolDepositOrder, error) {
	var row SolDepositOrder
	err := m.conn.QueryRowCtx(ctx, &row, "SELECT "+solDepositOrderRows+" FROM sol_deposit_order WHERE quote_idx=? FOR UPDATE", idx)
	return &row, err
}

func (m *customSolDepositOrderModel) MarkCredited(ctx context.Context, id, voucher int64, net decimal.Decimal) error {
	result, err := m.conn.ExecCtx(ctx, `UPDATE sol_deposit_order SET credit_voucher_id=?,credited_usd=?,credited_at=UTC_TIMESTAMP(3)
 WHERE id=? AND credit_voucher_id IS NULL`, voucher, net, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("SOL order %d credit marker affected %d rows", id, n)
	}
	return nil
}

type BeneficiaryPayment struct {
	QuoteIdx       string    `db:"quote_idx"`
	BeneficiaryIdx string    `db:"beneficiary_idx"`
	PayQuantity    string    `db:"pay_quantity"`
	TargetUSD      string    `db:"target_usd"`
	AcceptedAt     time.Time `db:"accepted_at"`
}

func (m *customSolDepositOrderModel) ListBeneficiaryPayments(ctx context.Context, payer int64, before string, limit int) ([]BeneficiaryPayment, error) {
	query := `SELECT o.quote_idx, q.snapshot_json->>'$.Beneficiary.Idx' AS beneficiary_idx,
     q.snapshot_json->>'$.PayQuantity' AS pay_quantity, q.snapshot_json->>'$.TargetUSD' AS target_usd, o.accepted_at
     FROM deposit_quote_confirmation q JOIN sol_deposit_order o ON o.quote_idx=q.quote_idx
     WHERE q.user_id=? AND o.accepted_at IS NOT NULL AND q.snapshot_json->>'$.Beneficiary.Idx' IS NOT NULL`
	args := []any{payer}
	if before != "" {
		query += ` AND o.id < (SELECT prev.id FROM sol_deposit_order prev JOIN deposit_quote_confirmation owner ON owner.quote_idx=prev.quote_idx WHERE prev.quote_idx=? AND owner.user_id=?)`
		args = append(args, before, payer)
	}
	query += " ORDER BY o.id DESC LIMIT ?"
	var rows []BeneficiaryPayment
	err := m.conn.QueryRowsCtx(ctx, &rows, query, append(args, limit)...)
	return rows, err
}
