package chainreceipt

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

// A Solidity receipt query may return [] for either an unknown or an empty
// height. Only a complete solid block header without transactions can advance.
func TestNileDiscoveryEmptyBlock(t *testing.T) {
	f, _ := tronSample(t, "tron-usdt")
	var block map[string]any
	if err := json.Unmarshal(f.Evidence.Block, &block); err != nil {
		t.Fatal(err)
	}
	delete(block, "transactions")
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = nileHistoryBlock(raw); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("nonempty transaction root accepted", err)
	}
	delete(block["block_header"].(map[string]any)["raw_data"].(map[string]any), "txTrieRoot")
	raw, err = json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := nileHistoryBlock(raw)
	if err != nil {
		t.Fatal(err)
	}
	asset := chainassets.NileUSDT(f.Receipt.Event.Mint)
	events, err := nileIncomingEvents(chainassets.NileHistory{Infos: []json.RawMessage{}}, empty, asset)
	if err != nil || len(events) != 0 {
		t.Fatal(events, err)
	}
	if _, err = nileIncomingEvents(chainassets.NileHistory{}, empty, asset); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("missing receipts accepted", err)
	}
	if _, err = nileHistoryBlock(json.RawMessage("{}")); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("unknown height accepted", err)
	}
}
