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

func TestLiveTONHistoricalReceipts(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_TON_RECEIPTS_LIVE") != "1" {
		t.Skip("explicit read-only live opt-in required")
	}
	endpoint := os.Getenv("DEXWALLET_CHAIN_TON_TESTNET_RPC_URL")
	if endpoint == "" {
		t.Fatal("explicit testnet endpoint required")
	}
	source, err := chainassets.New(chainassets.Config{Timeout: 30 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: endpoint, APIKey: os.Getenv("DEXWALLET_CHAIN_TON_TESTNET_API_KEY"), TestUSDTContract: tonTestMaster}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for _, tt := range []struct{ name, hash, asset, recipient, atoms string }{
		{"ton-native", "53d778fb27fe4ed339ba2514b5f3938964dcbc48b6b88a66fdea326cf5e25e98", "ton-testnet:ton", "0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp", "10000000"},
		{"ton-jetton", "d50f0d7f4d8691b09b8951c37306be48e0002e59bb7e68c25d1f19002025a230", "ton-testnet:usdt:" + tonTestMaster, "0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp", "1000000"},
		{"ton-no-state", "2b61c56d406a8bcafe9badb4ba06593655dfa81de141df1fb2cffa1435936096", "ton-testnet:ton", "0QCjk6WUaIpT9hONiPNEeomRCTzDh_BGPQrNlVKJ-tvveqrM", "50000000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := TONRequest{Network: chainassets.TONTestnet, Hash: tt.hash, AssetID: tt.asset, Recipient: tt.recipient, Atoms: tt.atoms}
			receipt, err := NewTONVerifier(source).Verify(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if path := os.Getenv("DEXWALLET_TEST_TON_RECEIPTS_EVIDENCE"); path != "" {
				data, err := json.MarshalIndent(struct {
					Request  TONRequest `json:"request"`
					Receipt  Receipt    `json:"receipt"`
					Evidence any        `json:"evidence"`
				}{r, receipt, receipt.evidence}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(path, tt.name+".json"), append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("masterchain=%d kind=%s atoms=%s recipientTransactionFee=%s", receipt.Transaction.Slot, receipt.Event.Kind, receipt.Event.Atoms, receipt.Transaction.FeeAtoms)
		})
	}
}
