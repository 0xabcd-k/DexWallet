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
	if sun <= 0 {
		return Stored{}, ErrInvalidRequest
	}
	asset := chainassets.Asset{ID: chainassets.TRONNile + ":trx", Origin: chainassets.Native, Decimals: 6}
	return s.recordPayout(ctx, hash, sender, recipient, strconv.FormatInt(sun, 10), signature, asset)
}

// RecordUSDTPayout reconciles a previously authorized direct transfer of this exact contract.
func (s *TRONService) RecordUSDTPayout(ctx context.Context, hash, sender, recipient, contract, atoms, signature string) (Stored, error) {
	if !tronUserAddress(contract) || !decimal256(atoms) {
		return Stored{}, ErrInvalidRequest
	}
	return s.recordPayout(ctx, hash, sender, recipient, atoms, signature, chainassets.NileUSDT(contract))
}

func (s *TRONService) recordPayout(ctx context.Context, hash, sender, recipient, atoms, signature string, asset chainassets.Asset) (Stored, error) {
	const network = chainassets.TRONNile
	if !tronHex(hash, 32) || !tronUserAddress(sender) || !tronUserAddress(recipient) || !tronHex(signature, 65) {
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
	r := TRONRequest{Network: network, Hash: hash, AssetID: asset.ID, Recipient: recipient, Atoms: atoms}
	receipt, err := verifyTRON(r, asset, proof)
	if err != nil {
		return Stored{}, err
	}
	var envelope struct{ Signature []string }
	if json.Unmarshal(proof.Transaction, &envelope) != nil || len(envelope.Signature) != 1 || !strings.EqualFold(envelope.Signature[0], signature) {
		return Stored{}, bad("solidified TRON signature differs from authorization")
	}
	if receipt.Event.SourceOwner != sender || receipt.Transaction.FeePayer != sender {
		return Stored{}, bad("TRON payout sender differs from authorization")
	}
	return s.store.record(ctx, receipt)
}
