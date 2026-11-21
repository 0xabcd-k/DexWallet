package chainreceipt

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestBaseDiscoveryTracePositionsAndReverts(t *testing.T) {
	f, _ := baseSample(t, "base-internal-eth")
	var root baseCall
	if err := json.Unmarshal(f.Evidence.CallTrace, &root); err != nil {
		t.Fatal(err)
	}
	recipient := f.Receipt.Event.RecipientOwner
	parent := baseCall{Type: "CALL", From: root.To, To: recipient, Value: "0x1"}
	extra := parent
	parent.Error = "execution reverted"
	parent.Calls = []baseCall{extra}
	static := baseCall{Type: "STATICCALL", Calls: []baseCall{extra}}
	root.Calls = append(root.Calls, parent, static, extra)
	pos := 0
	var events []baseIncoming
	if err := discoverBaseCalls(&root, f.Request.Hash, &pos, false, 0, &events); err != nil {
		t.Fatal(err)
	}
	var found []int
	for _, e := range events {
		if e.owner == recipient && e.kind == EVMCall {
			found = append(found, e.index)
		}
	}
	if len(found) != 2 || found[0] != 16 || found[1] != pos-1 || pos != 34 {
		t.Fatal(found, pos)
	}
}

func TestBaseDiscoveryMissingBlockEvidence(t *testing.T) {
	f, _ := baseSample(t, "base-usdc")
	b, err := historyBlock(f.Evidence.Block)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err = json.Unmarshal(f.Evidence.Receipt, &r); err != nil {
		t.Fatal(err)
	}
	// A single transaction block, retaining the real Transfer logIndex (5).
	b.Transactions = []string{f.Request.Hash}
	r["transactionIndex"] = "0x0"
	for _, item := range r["logs"].([]any) {
		item.(map[string]any)["transactionIndex"] = "0x0"
	}
	raw, _ := json.Marshal([]any{r})
	events, err := baseIncomingEvents(b, baseusdc.AssetID, raw, nil)
	if err != nil || len(events) != 1 || events[0].index != 5 || events[0].atoms != f.Request.Atoms {
		t.Fatal(events, err)
	}
	for _, change := range []func(map[string]any){
		func(r map[string]any) { delete(r, "to") },
		func(r map[string]any) { r["logs"] = nil },
		func(r map[string]any) { r["logs"].([]any)[1].(map[string]any)["removed"] = true },
		func(r map[string]any) { r["transactionHash"] = b.Hash },
	} {
		var rows []map[string]any
		if err = json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		change(rows[0])
		changed, _ := json.Marshal(rows)
		if _, err = baseIncomingEvents(b, baseusdc.AssetID, changed, nil); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		asset           string
		receipts, calls json.RawMessage
	}{
		{baseusdc.AssetID, []byte("[]"), nil}, {chainassets.BaseSepoliaETH, raw, []byte("[]")},
		{chainassets.BaseSepoliaETH, raw, []byte("[{}]")},
	} {
		if _, err = baseIncomingEvents(b, test.asset, test.receipts, test.calls); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(err)
		}
	}
}
