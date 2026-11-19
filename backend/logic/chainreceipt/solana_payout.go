package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
)

// Only explicit absence from both history queries may cause an original-message replay.
var ErrSolanaSignatureAbsent = errors.New("Solana signature absent from transaction and status history")

func (s *Service) RecordSolanaPayout(ctx context.Context, r Request, sender, blockhash string) (Stored, error) {
	if r.Network != chainassets.SolanaDevnet || r.Instruction != 0 || r.AssetID != r.Network+":sol" || !key(sender, 32) || !key(blockhash, 32) || !key(r.Signature, 64) || !key(r.Recipient, 32) || !atoms(r.Atoms) {
		return Stored{}, ErrInvalidRequest
	}
	// A receipt may already have committed before the payout marker. Recheck the
	// stored evidence against the exact authorized shape before contacting a node.
	var evidence chainassets.SolanaEvidence
	saved, err := model.NewChainObservedTransactionModel(s.conn).FindOneByNetworkSignature(ctx, r.Network, r.Signature)
	switch {
	case err == nil:
		err = json.Unmarshal([]byte(saved.EvidenceJson), &evidence)
		if err == nil && (len(evidence.Transaction) == 0 || null(evidence.Transaction)) {
			return Stored{}, bad("stored Solana payout transaction missing")
		}
	case errors.Is(err, model.ErrNotFound):
		evidence, err = s.verifier.source.ReadSolanaEvidence(ctx, r.Network, r.Signature)
	}
	if err != nil {
		return Stored{}, err
	}
	if null(evidence.Transaction) {
		var status statusResponse
		if json.Unmarshal(evidence.Status, &status) != nil || status.Context.Slot <= 0 || len(status.Value) != 1 {
			return Stored{}, bad("invalid Solana payout status history")
		}
		if status.Value[0] == nil {
			return Stored{}, ErrSolanaSignatureAbsent
		}
		// A node knows this signature, but has not returned the finalized transaction.
		return Stored{}, ErrUnconfirmed
	}
	asset, err := s.verifier.solanaAsset(r)
	if err != nil {
		return Stored{}, err
	}
	receipt, err := verify(r, asset, evidence)
	if err != nil {
		return Stored{}, err
	}
	var tx struct {
		Version     json.RawMessage
		Transaction struct {
			Signatures []string
			Message    struct {
				RecentBlockhash string
				AccountKeys     []account
				Instructions    []json.RawMessage
			}
		}
	}
	if err = json.Unmarshal(evidence.Transaction, &tx); err != nil {
		return Stored{}, err
	}
	msg := tx.Transaction.Message
	if string(tx.Version) != `"legacy"` || len(tx.Transaction.Signatures) != 1 || len(msg.AccountKeys) != 3 || len(msg.Instructions) != 1 ||
		msg.RecentBlockhash != blockhash || msg.AccountKeys[1].Pubkey != r.Recipient || msg.AccountKeys[2].Pubkey != SystemProgram || msg.AccountKeys[2].Writable ||
		receipt.Transaction.FeePayer != sender || receipt.Event.SourceOwner != sender {
		return Stored{}, bad("finalized payout differs from signed single SOL transfer")
	}
	return s.record(ctx, receipt)
}
