package transfer

import (
	"context"
	"fmt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ TransferOrderModel = (*customTransferOrderModel)(nil)

type (
	// TransferOrderModel is an interface to be customized, add more methods here,
	// and implement the added methods in customTransferOrderModel.
	TransferOrderModel interface {
		transferOrderModel
		WithSession(session sqlx.Session) TransferOrderModel
		Complete(context.Context, int64, int64) error
	}

	customTransferOrderModel struct {
		*defaultTransferOrderModel
	}
)

// NewTransferOrderModel returns a model for the database table.
func NewTransferOrderModel(conn sqlx.SqlConn) TransferOrderModel {
	return &customTransferOrderModel{
		defaultTransferOrderModel: newTransferOrderModel(conn),
	}
}

func (m *customTransferOrderModel) WithSession(session sqlx.Session) TransferOrderModel {
	return NewTransferOrderModel(sqlx.NewSqlConnFromSession(session))
}

// Complete binds the voucher within the transaction that inserted the order.
// There is no committed pending state and completed orders are immutable.
func (m *customTransferOrderModel) Complete(ctx context.Context, id, voucherID int64) error {
	result, err := m.conn.ExecCtx(ctx, "UPDATE transfer_order SET voucher_id=? WHERE id=? AND voucher_id IS NULL", voucherID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("transfer %d completion affected %d rows", id, n)
	}
	return nil
}
