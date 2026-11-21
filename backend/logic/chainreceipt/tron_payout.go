package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
)

var ErrTRONSolidAbsent = errors.New("TRON solidity node has neither transaction nor receipt")

// RecordNativePayout binds an already verified signed TRX transfer's hash to
// its solidified sender, beneficiary, principal and fee payer before persisting it.
func (s *TRONService) RecordNativePayout(ctx context.Context, hash, sender, recipient string, sun int64, signature string) (Stored, error) {
	const network = chainassets.TRONNile
	if !tronHex(hash, 32) || !tronUserAddress(sender) || !tronUserAddress(recipient) || sun <= 0 {
		return Stored{}, ErrInvalidRequest
	}
	var proof chainassets.TRONEvidence
	row, err := model.NewChainObservedTransactionModel(s.store.conn).FindOneByNetworkSignature(ctx, network, hash)
	switch {
	case err == nil:
		if err = json.Unmarshal([]byte(row.EvidenceJson), &proof); err != nil || tronEmptyObject(proof.Transaction) || tronEmptyObject(proof.Info) {
			return Stored{}, bad("invalid stored TRON payout evidence")
		}
	case errors.Is(err, model.ErrNotFound):
		proof, err = s.verifier.source.ReadTRONEvidence(ctx, network, hash)
		if err == nil && tronEmptyObject(proof.Transaction) && tronEmptyObject(proof.Info) {
			var genesis tronBlock
			if json.Unmarshal(proof.Genesis, &genesis) != nil || !genesis.valid() || genesis.ID != chainassets.TRONNileGenesis {
				return Stored{}, bad("wrong or missing Nile genesis")
			}
			return Stored{}, ErrTRONSolidAbsent
		}
	}
	if err != nil {
		return Stored{}, err
	}
	asset := chainassets.Asset{ID: network + ":trx", Origin: chainassets.Native, Decimals: 6}
	r := TRONRequest{Network: network, Hash: hash, AssetID: asset.ID, Recipient: recipient, Atoms: strconv.FormatInt(sun, 10)}
	receipt, err := verifyTRON(r, asset, proof)
	if err != nil {
		return Stored{}, err
	}
	var envelope struct{ Signature []string }
	if json.Unmarshal(proof.Transaction, &envelope) != nil || len(envelope.Signature) != 1 || !tronHex(signature, 65) || !strings.EqualFold(envelope.Signature[0], signature) {
		return Stored{}, bad("solidified TRX signature differs from authorization")
	}
	if receipt.Event.SourceOwner != sender || receipt.Transaction.FeePayer != sender {
		return Stored{}, bad("TRX payout sender differs from authorization")
	}
	return s.store.record(ctx, receipt)
}
