package chainsend

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
)

// Recovery operates only durable signed testnet messages. It owns scheduling
// metadata, while TON continues to own immutable signing/broadcast facts.
type Recovery struct {
	store    *recoveryStore
	executor *TON
	receipts *chainreceipt.TONService
}

func NewRecovery(db *sql.DB, executor *TON, receipts *chainreceipt.TONService, c RecoveryConfig) (*Recovery, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if db == nil || executor == nil || receipts == nil || executor.signer != nil {
		return nil, fmt.Errorf("TON recovery requires DB, receipts and an executor without signing capability")
	}
	w, _ := c.Wallet()
	if !bytes.Equal(w.PublicKey, executor.wallet.PublicKey) || w.SubwalletID != executor.wallet.SubwalletID || w.JettonMaster != executor.wallet.JettonMaster {
		return nil, ErrConflict
	}
	return &Recovery{newRecoveryStore(db, chainassets.TONTestnet, executor.sender, c.RecoveryPolicy), executor, receipts}, nil
}
func (r *Recovery) List(ctx context.Context, state string, limit int) ([]RecoveryView, error) {
	rows := []RecoveryView{}
	err := r.store.queryList(ctx, &rows, recoveryColumns, state, limit)
	return rows, err
}

// Retry authorizes another bounded observation/replay cycle of the SAME message.
// It never refreshes a signature, nonce, deadline or business request identity.
func (r *Recovery) Retry(ctx context.Context, key, operator, reason string) error {
	return r.store.retry(ctx, key, operator, reason)
}
func reviewRecovery(err error) bool {
	for _, cause := range []error{ErrInvalidRequest, ErrConflict, ErrInvalidSignature, ErrSigningUnavailable, chainreceipt.ErrInvalidRequest, chainreceipt.ErrInvalidEvidence, chainreceipt.ErrConflict, chainreceipt.ErrExecutionFailed, chainreceipt.ErrUnsupported} {
		if errors.Is(err, cause) {
			return true
		}
	}
	return false
}
func (r *Recovery) observe(ctx context.Context, claim recoveryClaim) (string, string, error) {
	key := claim.Key
	v, err := r.executor.store.Find(ctx, key)
	if err != nil {
		if reviewRecovery(err) {
			return RecoveryReview, "", err
		}
		return RecoveryReady, "", err
	}
	if v.State == Prepared {
		return RecoveryReview, "", ErrSigningUnavailable
	}
	received, err := r.executor.Reconcile(ctx, key, r.receipts)
	if err == nil {
		return RecoveryConfirmed, received.Receipt.Transaction.Signature, nil
	}
	if reviewRecovery(err) {
		return RecoveryReview, "", err
	}
	if !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		return RecoveryReady, "", err
	}
	// Always query before replay. An RPC outage, malformed/failed proof, an expired
	// authorization or an already accepted request must not trigger another send.
	if v.State != Submitted {
		if r.executor.now().Unix() >= int64(v.ValidUntil) {
			return RecoveryReady, "", errors.Join(err, ErrExpired)
		}
		if _, sendErr := r.executor.Execute(ctx, key); sendErr != nil {
			if reviewRecovery(sendErr) {
				return RecoveryReview, "", sendErr
			}
			return RecoveryReady, "", sendErr
		}
	}
	return RecoveryReady, "", err
}

// RunOne is an explicit one-shot operation. Daemon admission/draining is owned by
// RecoveryRuntime. All network work occurs after the short claim has committed.
func (r *Recovery) RunOne(ctx context.Context) (bool, error) {
	return r.store.runOne(ctx, r.observe)
}

func (r *Recovery) schedule() *recoveryStore { return r.store }
