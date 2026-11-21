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

var ErrRecoveryLeaseLost = errors.New("recovery claim no longer owned")
var ErrRecoveryBudget = errors.New("recovery attempt budget exhausted; inspect original payment")
var ErrRecoveryRetryDenied = errors.New("only a review record can resume original-message recovery")

type recoveryStore struct {
	conn    sqlx.SqlConn
	claimTx Transactor
	sender  string
	config  RecoveryPolicy
	// Table and predicates are fixed at construction by the chain implementation.
	table   string
	network string
	scope   string
	pending string
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
			err = tx.QueryRowCtx(ctx, &row, "SELECT id,request_key,recovery_state,recovery_attempts,recovery_generation FROM "+s.table+" WHERE "+s.scope+" AND recovery_state=? AND "+candidate.clock+"<=UTC_TIMESTAMP(6) AND "+s.pending+" ORDER BY "+candidate.clock+",id LIMIT 1 FOR UPDATE SKIP LOCKED", s.sender, candidate.state)
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
			_, err = tx.ExecCtx(ctx, "UPDATE "+s.table+" SET recovery_state=?,recovery_generation=?,recovery_lease_until=NULL,recovery_checked_at=UTC_TIMESTAMP(6),recovery_error=LEFT(CONCAT(recovery_error,'; ',?),2048),updated_at=UTC_TIMESTAMP(6) WHERE id=?", RecoveryReview, row.Generation, ErrRecoveryBudget.Error(), row.ID)
		} else {
			row.State = RecoveryRunning
			row.Attempts++
			_, err = tx.ExecCtx(ctx, "UPDATE "+s.table+" SET recovery_state=?,recovery_attempts=?,recovery_generation=?,recovery_lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE id=?", RecoveryRunning, row.Attempts, row.Generation, s.config.LeaseDuration.Microseconds(), row.ID)
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
	// The payout executors commit confirmation and clear the lease in
	// one update. A verified receipt takes precedence over any observation lease.
	if state == RecoveryConfirmed && s.network != chainassets.TONTestnet {
		return nil
	}
	receiptSet := ""
	args := []any{state, s.config.RetryInterval.Microseconds(), detail}
	if s.network == chainassets.TONTestnet {
		receiptSet = ",recovery_receipt_hash=?"
		args = append(args, receipt)
	}
	args = append(args, c.ID, c.Generation, RecoveryRunning)
	result, err := s.conn.ExecCtx(ctx, "UPDATE "+s.table+" SET recovery_state=?,recovery_lease_until=NULL,recovery_checked_at=UTC_TIMESTAMP(6),recovery_next_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),recovery_error=LEFT(?,2048)"+receiptSet+",updated_at=UTC_TIMESTAMP(6) WHERE id=? AND recovery_generation=? AND recovery_state=? AND recovery_lease_until>UTC_TIMESTAMP(6)", args...)
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
func (s *recoveryStore) queryList(ctx context.Context, out any, columns, state string, limit int) error {
	if limit < 1 || limit > 1000 {
		return ErrInvalidRequest
	}
	switch state {
	case "", RecoveryReady, RecoveryRunning, RecoveryConfirmed, RecoveryReview:
	default:
		return ErrInvalidRequest
	}
	query := "SELECT " + columns + " FROM " + s.table + " WHERE " + s.scope + " AND state<>?"
	args := []any{s.sender, Prepared}
	if state != "" {
		query += " AND recovery_state=?"
		args = append(args, state)
	}
	return s.conn.QueryRowsCtx(ctx, out, query+" ORDER BY id LIMIT ?", append(args, limit)...)
}
func (s *recoveryStore) retry(ctx context.Context, key, operator, reason string) error {
	if !requestKey.MatchString(key) || strings.TrimSpace(operator) == "" || strings.TrimSpace(reason) == "" || len(operator) > 128 || len(reason) > 256 {
		return fmt.Errorf("recovery retry requires original key, operator (up to 128 bytes) and reason (up to 256 bytes)")
	}
	note := fmt.Sprintf("operator=%s; reason=%s", operator, reason)
	result, err := s.conn.ExecCtx(ctx, "UPDATE "+s.table+" SET recovery_state=?,recovery_attempts=0,recovery_generation=recovery_generation+1,recovery_next_at=UTC_TIMESTAMP(6),recovery_retry_note=?,updated_at=UTC_TIMESTAMP(6) WHERE "+s.scope+" AND request_key=? AND recovery_state=?", RecoveryReady, note, s.sender, key, RecoveryReview)
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
func newRecoveryStore(db *sql.DB, network, sender string, policy RecoveryPolicy) *recoveryStore {
	s := &recoveryStore{conn: sqlx.NewSqlConnFromDB(db), claimTx: transaction.NewWithIsolation(db, sql.LevelReadCommitted), sender: sender, config: policy, network: network}
	s.scope, s.pending = "sender=?", "state IN ('"+Signed+"','"+Submitted+"')"
	switch network {
	case chainassets.TRONNile:
		s.table = "tron_payout"
	case chainassets.BaseSepolia:
		s.table = "base_payout"
	case chainassets.SolanaDevnet:
		s.table = "solana_payout"
	case chainassets.TONTestnet:
		s.table, s.scope, s.pending = "chain_send_intent", "network='"+chainassets.TONTestnet+"' AND sender=?", "state IN ('"+Signed+"','"+BroadcastUnknown+"','"+Submitted+"')"
	}
	return s
}

func (s *recoveryStore) runOne(ctx context.Context, observe func(context.Context, recoveryClaim) (string, string, error)) (bool, error) {
	c, err := s.claim(ctx)
	if err != nil || c == nil {
		return false, err
	}
	if c.State == RecoveryReview {
		return true, fmt.Errorf("%s recovery %s: %w", s.network, c.Key, ErrRecoveryBudget)
	}
	state, receipt, cause := observe(ctx, *c)
	if state == RecoveryReady && c.Attempts >= s.config.MaxAttempts {
		state = RecoveryReview
		cause = errors.Join(cause, ErrRecoveryBudget)
	}
	finishErr := s.finish(ctx, *c, state, receipt, cause)
	if err = errors.Join(cause, finishErr); err != nil {
		return true, fmt.Errorf("%s recovery %s: %w", s.network, c.Key, err)
	}
	return true, nil
}
