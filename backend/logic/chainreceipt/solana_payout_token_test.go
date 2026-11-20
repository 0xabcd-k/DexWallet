package chainreceipt

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

// Another transaction opens the authorized destination after prepare. The
// idempotent create becomes a no-op; charge only the fee observed in this tx.
func TestAuthorizedATAAlreadyCreatedHasZeroFunding(t *testing.T) {
	raw, err := os.ReadFile("testdata/spl-payout-usdc-new.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Receipt  Receipt
		Evidence chainassets.SolanaEvidence
		Intent   struct{ Sender, Recipient, Mint, TokenAtoms, Blockhash string }
	}
	if err = json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	p := capture.Intent
	transfer, err := solanaspl.New(p.Sender, p.Recipient, p.Mint, p.TokenAtoms, true)
	if err != nil {
		t.Fatal(err)
	}
	var tx map[string]any
	decoder := json.NewDecoder(bytes.NewReader(capture.Evidence.Transaction))
	decoder.UseNumber()
	if err = decoder.Decode(&tx); err != nil {
		t.Fatal(err)
	}
	meta := tx["meta"].(map[string]any)
	before, after := meta["preBalances"].([]any), meta["postBalances"].([]any)
	const dest = 1 // idempotent-create layout
	before[dest] = after[dest]
	paid, err := after[0].(json.Number).Int64()
	if err != nil {
		t.Fatal(err)
	}
	fee, err := meta["fee"].(json.Number).Int64()
	if err != nil {
		t.Fatal(err)
	}
	before[0] = paid + fee
	for _, item := range meta["postTokenBalances"].([]any) {
		row := item.(map[string]any)
		idx, err := row["accountIndex"].(json.Number).Int64()
		if err != nil {
			t.Fatal(err)
		}
		if idx == dest {
			copy := map[string]any{}
			for k, v := range row {
				copy[k] = v
			}
			copy["uiTokenAmount"] = map[string]any{"amount": "0", "decimals": solanaspl.Decimals}
			meta["preTokenBalances"] = append(meta["preTokenBalances"].([]any), copy)
		}
	}
	modified, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	funding, err := verifySPLPayout(transfer, capture.Receipt, p.Blockhash, modified)
	if err != nil || funding != 0 {
		t.Fatal(funding, err)
	}
}
