package chainsend

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type TRONRecovery struct {
	store    *recoveryStore
	executor *TRON
}

func NewTRONRecovery(db *sql.DB, node TRONNode, receipts *chainreceipt.TRONService, c TRONRecoveryConfig) (*TRONRecovery, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("TRON recovery requires a database")
	}
	executor, err := NewTRON(sqlx.NewSqlConnFromDB(db), c.Sender, node, receipts)
	if err != nil {
		return nil, err
	}
	return &TRONRecovery{newRecoveryStore(db, chainassets.TRONNile, executor.sender, c.RecoveryPolicy), executor}, nil
}

type TRONRecoveryView struct {
	Key               string        `db:"request_key" json:"key"`
	Sender            string        `db:"sender" json:"sender"`
	Recipient         string        `db:"recipient" json:"recipient"`
	AssetID           string        `db:"asset_id" json:"assetId"`
	Atoms             string        `db:"amount_atoms" json:"atoms"`
	TxID              string        `db:"tx_id" json:"txId"`
	ExpirationMs      int64         `db:"expiration_ms" json:"expirationMs"`
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

func (r *TRONRecovery) List(ctx context.Context, state string, limit int) ([]TRONRecoveryView, error) {
	rows := []TRONRecoveryView{}
	const columns = "request_key,sender,recipient,asset_id,IF(token_contract='',CAST(sun AS CHAR),token_atoms) AS amount_atoms,tx_id,expiration_ms,transfer_log_index,state,attempts,recovery_state,recovery_attempts,recovery_generation,recovery_next_at,recovery_lease_until,recovery_checked_at,recovery_error,recovery_retry_note"
	err := r.store.queryList(ctx, &rows, columns, state, limit)
	return rows, err
}
func (r *TRONRecovery) Retry(ctx context.Context, key, operator, reason string) error {
	return r.store.retry(ctx, key, operator, reason)
}
func (r *TRONRecovery) schedule() *recoveryStore { return r.store }
func (r *TRONRecovery) RunOne(ctx context.Context) (bool, error) {
	return r.store.runOne(ctx, r.observe)
}
func (r *TRONRecovery) observe(ctx context.Context, claim recoveryClaim) (string, string, error) {
	row, err := r.executor.step(ctx, claim.Key, &claim)
	switch {
	case err == nil && row.State == TRONConfirmed:
		return RecoveryConfirmed, "", nil
	case err == nil:
		return RecoveryReady, "", chainreceipt.ErrUnconfirmed
	case reviewRecovery(err):
		return RecoveryReview, "", err
	default:
		return RecoveryReady, "", err
	}
}
