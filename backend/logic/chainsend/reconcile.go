package chainsend

import (
	"context"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
)

// Reconcile works after expiry as well: expiry cannot disprove an earlier send.
// The receipt is committed before acceptance. A crash between commits is safe:
// the next call reuses the same immutable receipt and original signed request.
func (s *TON) Reconcile(ctx context.Context, key string, receipts *chainreceipt.TONService) (chainreceipt.TONSentStored, error) {
	r, err := s.store.load(ctx, key)
	if err != nil {
		return chainreceipt.TONSentStored{}, err
	}
	if r.State == Prepared {
		return chainreceipt.TONSentStored{}, chainreceipt.ErrUnconfirmed
	}
	body, err := s.checkedPayload(r)
	if err != nil {
		return chainreceipt.TONSentStored{}, err
	}
	if _, err = s.checkedArtifact(r, body); err != nil {
		return chainreceipt.TONSentStored{}, err
	}
	result, err := receipts.RecordSent(ctx, chainreceipt.TONSentRequest{Sender: r.Sender, Recipient: r.Recipient, Atoms: r.AmountAtoms, BodyHash: r.BodyHash})
	if err != nil {
		return result, err
	}
	if err = s.store.broadcastResult(ctx, r.Id, true, ""); err != nil {
		return result, fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return result, nil
}
