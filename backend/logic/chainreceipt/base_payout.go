package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Only explicit absence of both RPC results can allow replay of the original bytes.
var ErrBaseHashAbsent = errors.New("Base transaction and receipt both absent")

func (s *BaseService) RecordNativePayout(ctx context.Context, hash, sender, recipient, wei string) (Stored, error) {
	asset := chainassets.Asset{ID: chainassets.BaseSepoliaETH, Origin: chainassets.Native, Decimals: 18}
	return s.recordBasePayout(ctx, hash, sender, recipient, wei, asset)
}

func (s *BaseService) RecordUSDCPayout(ctx context.Context, hash, sender, recipient, atoms string) (Stored, error) {
	asset := chainassets.Asset{ID: baseusdc.AssetID, Contract: baseusdc.SepoliaContract, Origin: chainassets.Issuer, Decimals: 6}
	return s.recordBasePayout(ctx, hash, sender, recipient, atoms, asset)
}

func (s *BaseService) recordBasePayout(ctx context.Context, hash, sender, recipient, atoms string, asset chainassets.Asset) (Stored, error) {
	const network = chainassets.BaseSepolia
	hash, recipient = strings.ToLower(hash), strings.ToLower(recipient)
	r := BaseRequest{Network: network, Hash: hash, AssetID: asset.ID, Recipient: recipient, Atoms: atoms}
	if !hexData(hash, 32) || !hexData(sender, 20) || !hexData(recipient, 20) || !decimal256(atoms) {
		return Stored{}, ErrInvalidRequest
	}
	var evidence chainassets.BaseEvidence
	saved, err := model.NewChainObservedTransactionModel(s.store.conn).FindOneByNetworkSignature(ctx, network, hash)
	switch {
	case err == nil:
		err = json.Unmarshal([]byte(saved.EvidenceJson), &evidence)
		if err != nil || null(evidence.Transaction) || null(evidence.Receipt) {
			return Stored{}, bad("invalid stored Base payout evidence")
		}
	case errors.Is(err, model.ErrNotFound):
		evidence, err = s.verifier.source.ReadBaseEvidence(ctx, network, hash)
		if err == nil && null(evidence.Transaction) && null(evidence.Receipt) {
			return Stored{}, ErrBaseHashAbsent
		}
	}
	if err != nil {
		return Stored{}, err
	}
	if null(evidence.Transaction) || null(evidence.Receipt) {
		return Stored{}, ErrUnconfirmed
	}
	// Recompute the signed transaction hash from the node's typed fields.
	var tx types.Transaction
	if json.Unmarshal(evidence.Transaction, &tx) != nil || tx.Type() != types.DynamicFeeTxType || tx.Hash().Hex() != hash {
		return Stored{}, bad("Base payout signed transaction differs from authorization")
	}
	author, err := types.Sender(types.NewLondonSigner(big.NewInt(chainassets.BaseSepoliaChainID)), &tx)
	if err != nil || author != common.HexToAddress(sender) || len(tx.AccessList()) != 0 {
		return Stored{}, bad("unexpected Base payout signer or access list")
	}
	if asset.ID == baseusdc.AssetID {
		dest, amount, ok := baseusdc.ParseTransfer(&tx)
		if !ok || dest != common.HexToAddress(recipient) || amount.String() != atoms {
			return Stored{}, bad("Base payout calldata differs from authorized USDC transfer")
		}
	}
	receipt, err := verifyBase(r, asset, evidence)
	if err != nil {
		return Stored{}, err
	}
	if !strings.EqualFold(receipt.Event.SourceOwner, sender) || !strings.EqualFold(receipt.Transaction.FeePayer, sender) {
		return Stored{}, bad("Base payout sender differs from authorization")
	}
	return s.store.record(ctx, receipt)
}
