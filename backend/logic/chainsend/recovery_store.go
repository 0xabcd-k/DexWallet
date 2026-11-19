package chainsend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	RecoveryReady     = "ready"
	RecoveryRunning   = "running"
	RecoveryConfirmed = "confirmed" // A verified chain receipt, not a business settlement.
	RecoveryReview    = "review"    // Stops automation; never implies failed payment or a refund.
)

var ErrRecoveryLeaseLost = errors.New("TON recovery claim no longer owned")
var ErrRecoveryBudget = errors.New("TON recovery attempt budget exhausted; inspect original payment")
var ErrRecoveryRetryDenied = errors.New("only a review record can resume original-message recovery")

type recoveryStore struct {
	conn    sqlx.SqlConn
	claimTx Transactor
	sender  string
	config  RecoveryConfig
}
type recoveryClaim struct {
	ID         int64  `db:"id"`
	Key        string `db:"request_key"`
	State      string `db:"recovery_state"`
	Attempts   int64  `db:"recovery_attempts"`
	Generation int64  `db:"recovery_generation"`
}
type RecoveryView struct {
	Sender     string `db:"sender" json:"sender"`
	Recipient  string `db:"recipient" json:"recipient"`
	AssetID    string `db:"asset_id" json:"assetId"`
	Atoms      string `db:"amount_atoms" json:"atoms"`
	BodyHash   string `db:"body_hash" json:"bodyHash"`
	ValidUntil int64  `db:"valid_until" json:"validUntil"`

	Key         string       `db:"request_key" json:"key"`
	SendState   string       `db:"state" json:"sendState"`
	State       string       `db:"recovery_state" json:"state"`
	Attempts    int64        `db:"recovery_attempts" json:"attempts"`
	Generation  int64        `db:"recovery_generation" json:"generation"`
	NextAt      time.Time    `db:"recovery_next_at" json:"nextAt"`
	LeaseUntil  sql.NullTime `db:"recovery_lease_until" json:"leaseUntil"`
	CheckedAt   sql.NullTime `db:"recovery_checked_at" json:"checkedAt"`
	Error       string       `db:"recovery_error" json:"error"`
	ReceiptHash string       `db:"recovery_receipt_hash" json:"receiptHash"`
	RetryNote   string       `db:"recovery_retry_note" json:"retryNote"`
}

const recoveryColumns = "sender,recipient,asset_id,amount_atoms,body_hash,valid_until,request_key,state,recovery_state,recovery_attempts,recovery_generation,recovery_next_at,recovery_lease_until,recovery_checked_at,recovery_error,recovery_receipt_hash,recovery_retry_note"

func (s *recoveryStore) claim(ctx context.Context) (*recoveryClaim, error) {
	var out *recoveryClaim
	err := s.claimTx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		var row recoveryClaim
		var err error
		// READ COMMITTED plus separate indexes avoids locking every due item. No
		// RPC runs here; unknown claim commit leaves the lease for another process.
		for _, candidate := range []struct{ state, clock string }{{RecoveryRunning, "recovery_lease_until"}, {RecoveryReady, "recovery_next_at"}} {
			err = tx.QueryRowCtx(ctx, &row, "SELECT id,request_key,recovery_state,recovery_attempts,recovery_generation FROM chain_send_intent WHERE network=? AND sender=? AND recovery_state=? AND "+candidate.clock+"<=UTC_TIMESTAMP(6) AND state IN (?,?,?) ORDER BY "+candidate.clock+",id LIMIT 1 FOR UPDATE SKIP LOCKED", chainassets.TONTestnet, s.sender, candidate.state, Signed, BroadcastUnknown, Submitted)
			if !errors.Is(err, sqlx.ErrNotFound) {
				break
			}
		}
		if errors.Is(err, sqlx.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		row.Generation++
		if row.Attempts >= s.config.MaxAttempts {
			row.State = RecoveryReview
			_, err = tx.ExecCtx(ctx, "UPDATE chain_send_intent SET recovery_state=?,recovery_generation=?,recovery_lease_until=NULL,recovery_checked_at=UTC_TIMESTAMP(6),recovery_error=LEFT(CONCAT(recovery_error,'; ',?),2048),updated_at=UTC_TIMESTAMP(6) WHERE id=?", RecoveryReview, row.Generation, ErrRecoveryBudget.Error(), row.ID)
		} else {
			row.State = RecoveryRunning
			row.Attempts++
			_, err = tx.ExecCtx(ctx, "UPDATE chain_send_intent SET recovery_state=?,recovery_attempts=?,recovery_generation=?,recovery_lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE id=?", RecoveryRunning, row.Attempts, row.Generation, s.config.LeaseDuration.Microseconds(), row.ID)
		}
		if err == nil {
			out = &row
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *recoveryStore) finish(ctx context.Context, c recoveryClaim, state, receipt string, cause error) error {
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	result, err := s.conn.ExecCtx(ctx, "UPDATE chain_send_intent SET recovery_state=?,recovery_lease_until=NULL,recovery_checked_at=UTC_TIMESTAMP(6),recovery_next_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),recovery_error=LEFT(?,2048),recovery_receipt_hash=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND recovery_generation=? AND recovery_state=? AND recovery_lease_until>UTC_TIMESTAMP(6)", state, s.config.RetryInterval.Microseconds(), detail, receipt, c.ID, c.Generation, RecoveryRunning)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRecoveryLeaseLost
	}
	return nil
}
func (s *recoveryStore) list(ctx context.Context, state string, limit int) ([]RecoveryView, error) {
	if state != "" && state != RecoveryReady && state != RecoveryRunning && state != RecoveryConfirmed && state != RecoveryReview {
		return nil, ErrInvalidRequest
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidRequest
	}
	query := "SELECT " + recoveryColumns + " FROM chain_send_intent WHERE network=? AND sender=? AND state IN (?,?,?)"
	args := []any{chainassets.TONTestnet, s.sender, Signed, BroadcastUnknown, Submitted}
	if state != "" {
		query += " AND recovery_state=?"
		args = append(args, state)
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	rows := []RecoveryView{}
	err := s.conn.QueryRowsCtx(ctx, &rows, query, args...)
	return rows, err
}
func (s *recoveryStore) retry(ctx context.Context, key, operator, reason string) error {
	if !requestKey.MatchString(key) || strings.TrimSpace(operator) == "" || strings.TrimSpace(reason) == "" || len(operator) > 128 || len(reason) > 256 {
		return fmt.Errorf("recovery retry requires original key, operator (up to 128 bytes) and reason (up to 256 bytes)")
	}
	note := fmt.Sprintf("operator=%s; reason=%s", operator, reason)
	result, err := s.conn.ExecCtx(ctx, "UPDATE chain_send_intent SET recovery_state=?,recovery_attempts=0,recovery_generation=recovery_generation+1,recovery_next_at=UTC_TIMESTAMP(6),recovery_retry_note=?,updated_at=UTC_TIMESTAMP(6) WHERE network=? AND sender=? AND request_key=? AND recovery_state=?", RecoveryReady, note, chainassets.TONTestnet, s.sender, key, RecoveryReview)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRecoveryRetryDenied
	}
	return nil
}
func newRecoveryStore(db *sql.DB, sender string, c RecoveryConfig) *recoveryStore {
	return &recoveryStore{sqlx.NewSqlConnFromDB(db), transaction.NewWithIsolation(db, sql.LevelReadCommitted), sender, c}
}
