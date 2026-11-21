package chainreceipt

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestTONHistoryRequiresFinalizedConsecutiveAccountChain(t *testing.T) {
	raw, err := os.ReadFile("testdata/ton-collection-history.json")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string][]tonHistoryEntry
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	r := data["native"]
	owner, err := tonAddress("ton-testnet", r[0].Account)
	if err != nil {
		t.Fatal(err)
	}
	read := func(items []tonHistoryEntry, pos tonPosition) ([]tonCandidate, error) {
		b, e := json.Marshal(map[string]any{"transactions": items})
		if e != nil {
			t.Fatal(e)
		}
		return tonHistoryPageEntries(b, owner, pos)
	}
	first, err := read(r[:1], tonPosition{})
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	second := r[1]
	for _, edit := range []func(*tonHistoryEntry){
		func(e *tonHistoryEntry) { e.Account = "0:" + strings.Repeat("ac", 32) },
		func(e *tonHistoryEntry) { e.PrevHash = strings.Repeat("ab", 32) },
		func(e *tonHistoryEntry) { e.PrevLT = "0" },
		func(e *tonHistoryEntry) { e.LT = r[0].LT },
		func(e *tonHistoryEntry) { e.Hash = "invalid" },
	} {
		row := second
		edit(&row)
		if _, err = read([]tonHistoryEntry{row}, first[0].tonPosition); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(row, err)
		}
	}
	second.Finality = "pending"
	good, err := read([]tonHistoryEntry{r[0], second}, tonPosition{})
	if err != nil || len(good) != 1 || good[0] != first[0] {
		t.Fatal(good, err)
	}
	if _, err = read([]tonHistoryEntry{second}, first[0].tonPosition); !errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
	if _, err = tonHistoryPageEntries(json.RawMessage(`{}`), owner, tonPosition{}); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal(err)
	}
}

func TestTONDiscoveredAmountAndNotification(t *testing.T) {
	f, src := tonSample(t, "ton-collection-native")
	w := tonWork{tonCandidate: tonCandidate{tonPosition: tonPosition{lt: 111185131000003, hash: f.Request.Hash}, at: 1795257327}, asset: f.Request.AssetID, owner: f.Request.Recipient, account: f.Request.Recipient}
	r, err := discoveredTONRequest(w, TONIncoming, src.proof)
	if err != nil || r != f.Request {
		t.Fatal(r, err)
	}
	w.lt++
	if _, err = discoveredTONRequest(w, TONIncoming, src.proof); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal(err)
	}
	w.lt--
	editTON(&src.proof.Transaction, func(m map[string]any) { m["in_msg"].(map[string]any)["bounced"] = true })
	if _, err = discoveredTONRequest(w, TONIncoming, src.proof); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
