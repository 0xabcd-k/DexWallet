package baseusdc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestOnlyCanonicalDirectTransferIsAccepted(t *testing.T) {
	to := common.HexToAddress("0xecd14f7586f5053cecae148ae26309c2277d86a8")
	contract := common.HexToAddress(SepoliaContract)
	amount := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, tc := range []struct {
		name   string
		change func(*types.DynamicFeeTx)
	}{
		{"valid", func(*types.DynamicFeeTx) {}},
		{"wrong contract", func(tx *types.DynamicFeeTx) { tx.To = &to }},
		{"contract creation", func(tx *types.DynamicFeeTx) { tx.To = nil }},
		{"ETH attached", func(tx *types.DynamicFeeTx) { tx.Value = big.NewInt(1) }},
		{"approve", func(tx *types.DynamicFeeTx) { copy(tx.Data, common.FromHex("0x095ea7b3")) }},
		{"bad padding", func(tx *types.DynamicFeeTx) { tx.Data[4] = 1 }},
		{"zero recipient", func(tx *types.DynamicFeeTx) { clear(tx.Data[16:36]) }},
		{"zero amount", func(tx *types.DynamicFeeTx) { clear(tx.Data[36:]) }},
		{"truncated", func(tx *types.DynamicFeeTx) { tx.Data = tx.Data[:67] }},
		{"trailing data", func(tx *types.DynamicFeeTx) { tx.Data = append(tx.Data, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &types.DynamicFeeTx{To: &contract, Data: TransferData(to, amount)}
			tc.change(tx)
			dest, n, ok := ParseTransfer(types.NewTx(tx))
			if tc.name == "valid" {
				if !ok || dest != to || n.Cmp(amount) != 0 {
					t.Fatal(dest, n, ok)
				}
				return
			}
			if ok {
				t.Fatal("unexpectedly accepted transaction", tc.name)
			}
		})
	}
}
