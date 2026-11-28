package chainreceipt

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseeth"
	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Verify the original signed transaction, canonical finality, transfer and order nonce.
func VerifyBaseDeposit(e chainassets.BaseEvidence, tx *types.Transaction, q chainassets.DepositPreview, signature string) (Receipt, error) {
	var out Receipt
	if null(e.Transaction) && null(e.Receipt) {
		return out, ErrBaseHashAbsent
	}
	if null(e.Transaction) || null(e.Receipt) {
		return out, ErrUnconfirmed
	}
	a, err := q.BaseAuthorization()
	if err != nil {
		return out, err
	}
	data, err := a.Data(signature)
	if err != nil {
		return out, err
	}
	if err = chainassets.CheckBaseDepositWire(q, tx, data); err != nil {
		return out, err
	}
	var observed types.Transaction
	if json.Unmarshal(e.Transaction, &observed) != nil || observed.Hash() != tx.Hash() {
		return out, bad("Base deposit transaction differs from saved wire")
	}
	asset := chainassets.Asset{ID: baseusdc.AssetID, Contract: baseusdc.SepoliaContract, Origin: chainassets.Issuer, Decimals: 6}
	r := BaseRequest{Network: chainassets.BaseSepolia, AssetID: asset.ID, Hash: tx.Hash().Hex(), Recipient: strings.ToLower(a.To.Hex()), Atoms: a.Amount.String()}
	native, isETH := a.BaseProof.(baseeth.Redemption)
	if isETH {
		asset = chainassets.Asset{ID: baseeth.AssetID, Origin: chainassets.Native, Decimals: 18}
		r.AssetID = asset.ID
		index, err := nativeDepositCallIndex(e.CallTrace, native)
		if err != nil {
			return out, err
		}
		r.CallIndex = &index
	}
	out, err = verifyBase(r, asset, e)
	if err != nil {
		return out, err
	}
	var receipt baseReceipt
	if err := json.Unmarshal(e.Receipt, &receipt); err != nil {
		return out, err
	}
	price, err := quantity(strings.ToLower(receipt.EffectiveGasPrice))
	if err != nil || price.Cmp(tx.GasFeeCap()) > 0 {
		return out, bad("effective gas price exceeds signed fee cap")
	}
	if !strings.EqualFold(out.Event.SourceOwner, q.WalletAddress) || !strings.EqualFold(out.Transaction.FeePayer, q.FeePayer) {
		return out, bad("unexpected Base deposit sender")
	}
	if isETH {
		return out, verifyRedemptionEvent(receipt, native, signature)
	}
	ids, err := baseDepositReferences(receipt, q.WalletAddress)
	if err != nil || len(ids) != 1 || ids[0] != q.QuoteIdx {
		return out, bad("missing exact USDC AuthorizationUsed event")
	}
	return out, nil
}
func baseDepositReferences(receipt baseReceipt, owner string) ([]string, error) {
	if receipt.Logs == nil {
		return nil, errors.New("missing Base deposit logs")
	}
	var ids []string
	for _, l := range *receipt.Logs {
		if !strings.EqualFold(l.Address, baseusdc.SepoliaContract) || len(l.Topics) != 3 || !strings.EqualFold(l.Topics[0], baseusdc.AuthorizationUsedTopic.Hex()) || !strings.EqualFold(l.Topics[1], common.BytesToHash(common.HexToAddress(owner).Bytes()).Hex()) {
			continue
		}
		if !hexData(strings.ToLower(l.Topics[2]), 32) || l.Data != "0x" {
			return nil, bad("malformed Base authorization nonce")
		}
		if idx, ok := baseusdc.DepositIdx(common.HexToHash(l.Topics[2])); ok {
			ids = append(ids, idx)
		}
	}
	return ids, nil
}
