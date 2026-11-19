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

// Historical, read-only Nile queries. No wallet keys, signing or broadcasting.
func TestLiveTRONHistoricalReceipts(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TRON_RECEIPTS_LIVE") != "1" {
		t.Skip("explicit read-only live opt-in required")
	}
	endpoint := os.Getenv("DEXWALLET_CHAIN_TRON_NILE_RPC_URL")
	if endpoint == "" {
		t.Fatal("explicit Nile endpoint required")
	}
	for _, tt := range []struct{ name, hash, contract, atoms string }{
		{"tron-trx", "c08fc0d615c4dc8546ce592e2d3bdbe711c282c68b437c18549395ac1979ac3e", "", "1000"},
		{"tron-usdt", "caff6737825af0e5dddb53d148336956a0a62c5a6fa67ff7385f2fe00c2b3006", "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf", "10000"},
		{"tron-usdt-fixture", "82734cb0e229e0f5c8fe3716fd8ccee522e49a603afa1a1e6654f52cc49f2182", "TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn", "10000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: endpoint, TestUSDTContract: tt.contract}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			r := TRONRequest{Network: chainassets.TRONNile, Hash: tt.hash, AssetID: "tron-nile:trx", Recipient: "TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH", Atoms: tt.atoms}
			if tt.contract != "" {
				index := 0
				r.LogIndex = &index
				r.AssetID = "tron-nile:usdt:" + tt.contract
			}
			v, err := NewTRONVerifier(source).Verify(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if path := os.Getenv("DEXWALLET_TEST_TRON_RECEIPTS_EVIDENCE"); path != "" {
				data, err := json.MarshalIndent(struct {
					Request  TRONRequest `json:"request"`
					Receipt  Receipt     `json:"receipt"`
					Evidence any         `json:"evidence"`
				}{r, v, v.evidence}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(path, tt.name+".json"), append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var info tronInfo
			_ = json.Unmarshal(v.evidence.(chainassets.TRONEvidence).Info, &info)
			t.Logf("solid block=%d kind=%s index=%d atoms=%s burnedSun=%s bandwidth=%d energy=%d originEnergy=%d", v.Transaction.Slot, v.Event.Kind, v.Event.Instruction, v.Event.Atoms, v.Transaction.FeeAtoms, info.Receipt.NetUsage, info.Receipt.EnergyUsageTotal, info.Receipt.OriginEnergyUsage)
		})
	}
}
