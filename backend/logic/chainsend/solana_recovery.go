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

type SolanaRecovery struct {
	store    *recoveryStore
	executor *Solana
}

func NewSolanaRecovery(db *sql.DB, node SolanaNode, receipts *chainreceipt.Service, c SolanaRecoveryConfig) (*SolanaRecovery, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("Solana recovery requires a database")
	}
	executor, err := NewSolana(sqlx.NewSqlConnFromDB(db), c.Sender, node, receipts)
	if err != nil {
		return nil, err
	}
	return &SolanaRecovery{newRecoveryStore(db, chainassets.SolanaDevnet, c.Sender, c.RecoveryPolicy), executor}, nil
}

type SolanaRecoveryView struct {
	Key             string       `db:"request_key" json:"key"`
	Sender          string       `db:"sender" json:"sender"`
	Recipient       string       `db:"recipient" json:"recipient"`
	AssetID         string       `db:"asset_id" json:"assetId"`
	Atoms           string       `db:"amount_atoms" json:"atoms"`
	Signature       string       `db:"signature" json:"signature"`
	LastValidHeight int64        `db:"last_valid_height" json:"lastValidBlockHeight"`
	SendState       string       `db:"state" json:"sendState"`
	State           string       `db:"recovery_state" json:"state"`
	Attempts        int64        `db:"recovery_attempts" json:"attempts"`
	Generation      int64        `db:"recovery_generation" json:"generation"`
	NextAt          time.Time    `db:"recovery_next_at" json:"nextAt"`
	LeaseUntil      sql.NullTime `db:"recovery_lease_until" json:"leaseUntil"`
	CheckedAt       sql.NullTime `db:"recovery_checked_at" json:"checkedAt"`
	Error           string       `db:"recovery_error" json:"error"`
	RetryNote       string       `db:"recovery_retry_note" json:"retryNote"`
}

func (r *SolanaRecovery) List(ctx context.Context, state string, limit int) ([]SolanaRecoveryView, error) {
	rows := []SolanaRecoveryView{}
	const columns = "request_key,sender,recipient,asset_id,IF(mint='',lamports,token_atoms) AS amount_atoms,signature,last_valid_height,state,recovery_state,recovery_attempts,recovery_generation,recovery_next_at,recovery_lease_until,recovery_checked_at,recovery_error,recovery_retry_note"
	err := r.store.queryList(ctx, &rows, columns, state, limit)
	return rows, err
}
func (r *SolanaRecovery) Retry(ctx context.Context, key, operator, reason string) error {
	return r.store.retry(ctx, key, operator, reason)
}
func (r *SolanaRecovery) schedule() *recoveryStore { return r.store }
func (r *SolanaRecovery) RunOne(ctx context.Context) (bool, error) {
	return r.store.runOne(ctx, r.observe)
}
func (r *SolanaRecovery) observe(ctx context.Context, claim recoveryClaim) (string, string, error) {
	row, err := r.executor.step(ctx, claim.Key, &claim)
	switch {
	case err == nil && row.State == SolanaConfirmed:
		return RecoveryConfirmed, "", nil
	case err == nil:
		// Broadcast acknowledgement or a concurrent submission still needs a receipt.
		return RecoveryReady, "", chainreceipt.ErrUnconfirmed
	case reviewRecovery(err) || errors.Is(err, ErrSolanaIntent) || errors.Is(err, ErrSolanaSignature):
		return RecoveryReview, "", err
	default:
		return RecoveryReady, "", err
	}
}
