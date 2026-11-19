//go:build integration

package chainreceipt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

// Historical testnet payments only: no keys, faucet, signing or broadcasting.
func TestLiveHistoricalReceipts(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_CHAIN_RECEIPTS_LIVE") != "1" {
		t.Skip("explicit read-only live opt-in required")
	}
	c := chainassets.Config{Timeout: 15 * time.Second}
	c.LoadEnvironment()
	source, err := chainassets.New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	v := NewVerifier(source)
	cases := []struct {
		name, signature, asset, atoms string
		index                         int
	}{
		{"sol", "3uechSmrGT1Axu3zhxVj6URgbMgoLXLyzSd1rS2EnJ88LpRRfMkB8ahZ4Csh13o6VzFmiyoAnHmmoMQ2yYxv5FJn", "solana-devnet:sol", "1000", 0},
		{"usdc", "4KyfkNqosDFzAd4sAQwmGEtqEmmGZmp7hHFs8BNz5Za5TpuL9rREueodng2L1wNvpJgzmdhBdSkk2BQFxpqLf7pf", "solana-devnet:usdc", "10000", 1},
		{"usdt-fixture", "4589sfUuPKo6gsG9dqppgxtCgiFwkePZg4doFPdQp2rKgbE4BjEm7mmGoCUAkAgQeWbJhQ4guBc3xbYTeTNP8nK5", "solana-devnet:usdt:2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5", "10000", 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			request := Request{Network: chainassets.SolanaDevnet, Signature: tt.signature, Instruction: tt.index, AssetID: tt.asset, Recipient: receiver, Atoms: tt.atoms}
			receipt, err := v.Verify(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if path := os.Getenv("DEXWALLET_TEST_CHAIN_RECEIPTS_EVIDENCE"); path != "" {
				b, err := json.MarshalIndent(struct {
					Request  Request `json:"request"`
					Receipt  Receipt `json:"receipt"`
					Evidence any     `json:"evidence"`
				}{request, receipt, receipt.evidence}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(path, tt.name+".json"), append(b, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("finalized %s: slot=%d instruction=%d atoms=%s fee=%s", tt.name, receipt.Transaction.Slot, receipt.Event.Instruction, receipt.Event.Atoms, receipt.Transaction.FeeAtoms)
		})
	}
}
