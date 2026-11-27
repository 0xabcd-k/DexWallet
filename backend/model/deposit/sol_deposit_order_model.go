package deposit

import (
	"context"
	"database/sql"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ SolDepositOrderModel = (*customSolDepositOrderModel)(nil)

type (
	// SolDepositOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customSolDepositOrderModel.
	SolDepositOrderModel interface {
		solDepositOrderModel
		AdvanceExecution(context.Context, *SolDepositOrder, string, sql.NullString, string, int64, int64) error
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
func (m *customSolDepositOrderModel) AdvanceExecution(ctx context.Context, old *SolDepositOrder, state string, sig sql.NullString, policy string, fee, slot int64) error {
	_, err := m.conn.ExecCtx(ctx, `UPDATE sol_deposit_order SET execution_state=?,tx_signature=?,sponsor_policy=?,actual_lamports_fee=?,finalized_slot=?
 WHERE quote_idx=? AND accepted_at IS NOT NULL AND execution_state=? AND tx_signature <=> ?`, state, sig, policy, fee, slot, old.QuoteIdx, old.ExecutionState, old.TxSignature)
	return err
}
