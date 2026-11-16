package chainreceipt

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// The unique-key write locks one transaction identity. Every event writer takes
// this lock first, so different instructions in the same tx cannot race its facts.
func LockTransaction(ctx context.Context, conn sqlx.SqlConn, row *ChainObservedTransaction) (*ChainObservedTransaction, error) {
	_, err := conn.ExecCtx(ctx, `INSERT INTO chain_observed_transaction(network,signature,slot,fee_payer,fee_atoms,fingerprint,evidence_json) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, row.Network, row.Signature, row.Slot, row.FeePayer, row.FeeAtoms, row.Fingerprint, row.EvidenceJson)
	if err != nil {
		return nil, err
	}
	var found ChainObservedTransaction
	err = conn.QueryRowCtx(ctx, &found, "SELECT "+chainObservedTransactionRows+" FROM chain_observed_transaction WHERE network=? AND signature=? FOR UPDATE", row.Network, row.Signature)
	return &found, err
}
