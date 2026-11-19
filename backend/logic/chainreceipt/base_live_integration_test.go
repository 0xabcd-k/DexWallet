//go:build integration

package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestLiveBaseHistoricalReceipts(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_RECEIPTS_LIVE") != "1" {
		t.Skip("explicit read-only live opt-in required")
	}
	c := chainassets.Config{Timeout: 30 * time.Second}
	c.LoadEnvironment()
	source, err := chainassets.New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	index := 5
	for _, tt := range []struct {
		name        string
		request     BaseRequest
		unsupported bool
	}{
		{"base-eth", BaseRequest{Network: chainassets.BaseSepolia, Hash: "0xdc7f376ee9736fa371a93e90c8c4b3c33c12cc3c4720b1781ad2bd4c135a76a8", AssetID: "base-sepolia:eth", Recipient: "0xecd14F7586f5053CECae148Ae26309C2277D86A8", Atoms: "1000000000000"}, false},
		{"base-usdc", BaseRequest{Network: chainassets.BaseSepolia, Hash: "0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17", LogIndex: &index, AssetID: "base-sepolia:usdc", Recipient: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA", Atoms: "10000"}, false},
		{"base-internal-eth", BaseRequest{Network: chainassets.BaseSepolia, Hash: "0x1c3509c224c12725106e47b57e5f5627934edf229ae51fb42861173fb36c2852", AssetID: "base-sepolia:eth", Recipient: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA", Atoms: "1000000000000"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewBaseVerifier(source).Verify(context.Background(), tt.request)
			if tt.unsupported {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatal(err)
				}
				t.Log("internal ETH explicitly unsupported")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if path := os.Getenv("DEXWALLET_TEST_BASE_RECEIPTS_EVIDENCE"); path != "" {
				data, err := json.MarshalIndent(struct {
					Request  BaseRequest `json:"request"`
					Receipt  Receipt     `json:"receipt"`
					Evidence any         `json:"evidence"`
				}{tt.request, v, v.evidence}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(path, tt.name+".json"), append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("finalized block=%d kind=%s index=%d atoms=%s fullFee=%s L2=%s L1=%s operator=%s", v.Transaction.Slot, v.Event.Kind, v.Event.Instruction, v.Event.Atoms, v.Transaction.FeeAtoms, v.Transaction.ExecutionFeeAtoms, v.Transaction.L1FeeAtoms, v.Transaction.OperatorFeeAtoms)
		})
	}
}
