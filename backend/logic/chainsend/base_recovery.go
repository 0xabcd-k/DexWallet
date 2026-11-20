package chainsend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type BaseRecovery struct {
	store    *recoveryStore
	executor *Base
}

func NewBaseRecovery(db *sql.DB, node BaseNode, receipts *chainreceipt.BaseService, c BaseRecoveryConfig) (*BaseRecovery, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("Base recovery requires a database")
	}
	executor, err := NewBase(sqlx.NewSqlConnFromDB(db), c.Sender, node, receipts)
	if err != nil {
		return nil, err
	}
	return &BaseRecovery{newRecoveryStore(db, chainassets.BaseSepolia, executor.sender, c.RecoveryPolicy), executor}, nil
}

type BaseRecoveryView struct {
	Key               string        `db:"request_key" json:"key"`
	Sender            string        `db:"sender" json:"sender"`
	Recipient         string        `db:"recipient" json:"recipient"`
	AssetID           string        `db:"asset_id" json:"assetId"`
	Atoms             string        `db:"amount_atoms" json:"atoms"`
	TxHash            string        `db:"tx_hash" json:"txHash"`
	Nonce             string        `db:"nonce" json:"nonce"`
	TransferLogIndex  sql.NullInt64 `db:"transfer_log_index" json:"transferLogIndex"`
	SendState         string        `db:"state" json:"sendState"`
	BroadcastAttempts int64         `db:"attempts" json:"broadcastAttempts"`
	State             string        `db:"recovery_state" json:"state"`
	Attempts          int64         `db:"recovery_attempts" json:"attempts"`
	Generation        int64         `db:"recovery_generation" json:"generation"`
	NextAt            time.Time     `db:"recovery_next_at" json:"nextAt"`
	LeaseUntil        sql.NullTime  `db:"recovery_lease_until" json:"leaseUntil"`
	CheckedAt         sql.NullTime  `db:"recovery_checked_at" json:"checkedAt"`
	Error             string        `db:"recovery_error" json:"error"`
	RetryNote         string        `db:"recovery_retry_note" json:"retryNote"`
}

func (r *BaseRecovery) List(ctx context.Context, state string, limit int) ([]BaseRecoveryView, error) {
	rows := []BaseRecoveryView{}
	const columns = "request_key,sender,recipient,asset_id,IF(token_atoms='',wei_amount,token_atoms) AS amount_atoms,tx_hash,nonce,transfer_log_index,state,attempts,recovery_state,recovery_attempts,recovery_generation,recovery_next_at,recovery_lease_until,recovery_checked_at,recovery_error,recovery_retry_note"
	err := r.store.queryList(ctx, &rows, columns, state, limit)
	return rows, err
}
func (r *BaseRecovery) Retry(ctx context.Context, key, operator, reason string) error {
	return r.store.retry(ctx, key, operator, reason)
}
func (r *BaseRecovery) schedule() *recoveryStore { return r.store }
func (r *BaseRecovery) RunOne(ctx context.Context) (bool, error) {
	return r.store.runOne(ctx, r.observe)
}
func (r *BaseRecovery) observe(ctx context.Context, claim recoveryClaim) (string, string, error) {
	row, err := r.executor.step(ctx, claim.Key, &claim)
	switch {
	case err == nil && row.State == BaseConfirmed:
		return RecoveryConfirmed, "", nil
	case err == nil:
		return RecoveryReady, "", chainreceipt.ErrUnconfirmed
	case reviewRecovery(err) || errors.Is(err, ErrBaseNonceChanged) || errors.Is(err, ErrUnsupportedBaseToken):
		return RecoveryReview, "", err
	default:
		return RecoveryReady, "", err
	}
}
