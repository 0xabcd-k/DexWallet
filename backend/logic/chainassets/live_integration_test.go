//go:build integration

package chainassets

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Opt-in, read-only public testnet queries. It accepts no secrets or transactions.
// Endpoint/test-token configuration uses the same environment names as the API.
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_CHAIN_ASSETS_LIVE") != "1" {
		t.Skip("explicit read-only live opt-in required")
	}
	c := Config{}
	c.LoadEnvironment()
	s, err := New(c, allow)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owners := map[string]string{TONTestnet: "0QCjk6WUaIpT9hONiPNEeomRCTzDh_BGPQrNlVKJ-tvveqrM", BaseSepolia: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA", SolanaDevnet: solOwner, TRONNile: tronOwner}
	var evidence []Result
	for _, n := range s.Catalog() {
		if !n.Testnet || !n.Enabled {
			continue
		}
		t.Run(n.ID, func(t *testing.T) {
			r, err := s.Query(context.Background(), 1, n.ID, owners[n.ID])
			if err != nil {
				t.Fatal(err)
			}
			evidence = append(evidence, r)
			for _, b := range r.Items {
				if b.Status != StatusOK {
					t.Errorf("%s: %s", b.AssetID, b.ErrorCode)
				}
			}
		})
	}
	if len(evidence) == 0 {
		t.Fatal("no testnet configured")
	}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	t.Log(string(raw))
	if path := os.Getenv("DEXWALLET_TEST_CHAIN_ASSETS_EVIDENCE"); path != "" {
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
