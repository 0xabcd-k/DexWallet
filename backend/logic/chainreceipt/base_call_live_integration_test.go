//go:build integration

package chainreceipt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

// Separate opt-in: a normal Base RPC may not expose historical execution traces.
func TestLiveBaseInternalETHReceipt(t *testing.T) {
	if os.Getenv("DEXWALLET_TEST_BASE_CALL_RECEIPTS_LIVE") != "1" {
		t.Skip("explicit read-only historical trace opt-in required")
	}
	c := chainassets.Config{Timeout: 30 * time.Second}
	c.LoadEnvironment()
	source, err := chainassets.New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := BaseRequest{Network: chainassets.BaseSepolia, Hash: "0x1c3509c224c12725106e47b57e5f5627934edf229ae51fb42861173fb36c2852", AssetID: "base-sepolia:eth", Recipient: "0xa59c992aa84657df4ebff6f04e11a1632f6db1fa", Atoms: "1000000000000"}
	proof, err := source.ReadBaseCallEvidence(context.Background(), r.Network, r.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if null(proof.Transaction) || null(proof.Receipt) {
		t.Fatal(ErrUnconfirmed)
	}
	if null(proof.CallTrace) {
		t.Fatal("historical node did not return a complete call trace")
	}
	// Discover the position from the complete node response, then run all normal
	// validations on that exact response. No claimed browser index is trusted.
	var root baseCall
	if err = json.Unmarshal(proof.CallTrace, &root); err != nil {
		t.Fatal(err)
	}
	position, matches := 0, 0
	var walk func(baseCall)
	walk = func(call baseCall) {
		n, e := quantity(call.Value)
		if position > 0 && call.Type == "CALL" && strings.EqualFold(call.From, "0xecd14f7586f5053cecae148ae26309c2277d86a8") && strings.EqualFold(call.To, r.Recipient) && e == nil && n.String() == r.Atoms {
			i := position
			r.CallIndex = &i
			matches++
		}
		position++
		for _, child := range call.Calls {
			walk(child)
		}
	}
	walk(root)
	if matches != 1 {
		t.Fatalf("expected one historical ETH transfer, found %d", matches)
	}
	cached := &baseFixtureSource{catalog: source.Catalog(), proof: proof}
	v, err := NewBaseVerifier(cached).Verify(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if v.Transaction.Slot != 53792630 || v.Transaction.FeeAtoms != "1935996009267" || v.Event.SourceOwner == v.Transaction.FeePayer {
		t.Fatal(v)
	}
	if path := os.Getenv("DEXWALLET_TEST_BASE_RECEIPTS_EVIDENCE"); path != "" {
		data, err := json.MarshalIndent(struct {
			Request  BaseRequest              `json:"request"`
			Receipt  Receipt                  `json:"receipt"`
			Evidence chainassets.BaseEvidence `json:"evidence"`
		}{r, v, proof}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(path, "base-internal-eth.json"), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("finalized block=%d callIndex=%d atoms=%s actualPayer=%s fullFee=%s", v.Transaction.Slot, v.Event.Instruction, v.Event.Atoms, v.Transaction.FeePayer, v.Transaction.FeeAtoms)
}
