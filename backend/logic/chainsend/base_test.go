package chainsend

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestBaseSignatureBindsTransferAndFees(t *testing.T) {
	owner, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	to := crypto.PubkeyToAddress(other.PublicKey)
	row := model.BasePayout{Sender: strings.ToLower(crypto.PubkeyToAddress(owner.PublicKey).Hex()), Recipient: to.Hex(), ChainId: chainassets.BaseSepoliaChainID, Nonce: "6", WeiAmount: "123", GasLimit: 25000, FeeCap: "30000000", TipCap: "1000000"}
	unsigned, err := baseUnsigned(&row)
	if err != nil {
		t.Fatal(err)
	}
	row.SigningHash = baseSigner.Hash(unsigned).Hex()
	for _, tc := range []struct {
		name string
		edit func(*types.DynamicFeeTx)
	}{
		{"valid", func(*types.DynamicFeeTx) {}},
		{"another-signer", func(*types.DynamicFeeTx) {}},
		{"network", func(tx *types.DynamicFeeTx) { tx.ChainID = big.NewInt(8453) }},
		{"nonce", func(tx *types.DynamicFeeTx) { tx.Nonce++ }},
		{"recipient", func(tx *types.DynamicFeeTx) { dest := common.HexToAddress(row.Sender); tx.To = &dest }},
		{"amount", func(tx *types.DynamicFeeTx) { tx.Value = big.NewInt(124) }},
		{"gas-limit", func(tx *types.DynamicFeeTx) { tx.Gas++ }},
		{"fee-cap", func(tx *types.DynamicFeeTx) { tx.GasFeeCap = big.NewInt(30000001) }},
		{"tip-cap", func(tx *types.DynamicFeeTx) { tx.GasTipCap = big.NewInt(1) }},
		{"calldata", func(tx *types.DynamicFeeTx) { tx.Data = []byte{1} }},
		{"access-list", func(tx *types.DynamicFeeTx) { tx.AccessList = types.AccessList{{Address: to}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &types.DynamicFeeTx{ChainID: unsigned.ChainId(), Nonce: unsigned.Nonce(), To: unsigned.To(), Gas: unsigned.Gas(), Value: unsigned.Value(), GasFeeCap: unsigned.GasFeeCap(), GasTipCap: unsigned.GasTipCap()}
			tc.edit(data)
			key := owner
			if tc.name == "another-signer" {
				key = other
			}
			signed, err := types.SignNewTx(key, types.NewLondonSigner(data.ChainID), data)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := signed.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			got, err := baseSigned(&row, hexutil.Encode(wire))
			if tc.name == "valid" {
				if err != nil || got.Hash() != signed.Hash() {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidSignature) {
				t.Fatal("accepted altered authorization", err)
			}
		})
	}
}

func TestBasePayoutAddressChecks(t *testing.T) {
	const valid = "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
	for _, address := range []string{valid, strings.ToLower(valid), "0x" + strings.ToUpper(valid[2:])} {
		got, err := baseAddress(address)
		if err != nil || got != strings.ToLower(valid) {
			t.Fatal(address, got, err)
		}
	}
	for _, address := range []string{"0xa59c992Aa84657df4EbFF6F04E11a1632f6DB1FA", valid[2:], "0x" + strings.Repeat("0", 40)} {
		if _, err := baseAddress(address); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(address, err)
		}
	}
}
