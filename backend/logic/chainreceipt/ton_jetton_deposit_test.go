package chainreceipt

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestJettonDepositRequiresFinalRefundAndCompleteBoundTrace(t *testing.T) {
	raw, err := os.ReadFile("testdata/ton-self-deposit-jetton-repeat.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Quote                            chainassets.DepositPreview
		UserSignature, PlatformSignature string
		Evidence                         chainassets.TONEvidence
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	u, _ := hex.DecodeString(p.UserSignature)
	s, _ := hex.DecodeString(p.PlatformSignature)
	relay, err := p.Quote.TONRelay(u, s)
	if err != nil {
		t.Fatal(err)
	}
	receipt, actual, costs, err := VerifyTONJettonDeposit(p.Evidence, relay)
	if err != nil || receipt.Event.Atoms != "17443" || actual.Int64() != 3546798 || costs.Excess <= 0 || costs.SenderPaid <= 0 {
		t.Fatal("final accounting", err, costs)
	}
	for _, change := range []string{"no refund", "different refund owner", "unfinalized refund", "unrelated user transfer", "provisional trace", "no recipient block", "wrong master wallet"} {
		t.Run(change, func(t *testing.T) {
			e := p.Evidence
			var index struct{ Traces []map[string]any }
			if err := json.Unmarshal(e.Trace, &index); err != nil {
				t.Fatal(err)
			}
			trace := index.Traces[0]
			rows := trace["transactions"].(map[string]any)
			for h, v := range rows {
				tx := v.(map[string]any)
				in := tx["in_msg"].(map[string]any)
				if in["opcode"] == "0xd53276db" {
					switch change {
					case "no refund":
						delete(rows, h)
						trace["trace_info"].(map[string]any)["transactions"] = len(rows)
					case "different refund owner":
						tx["account"] = relay.Sender
						in["destination"] = relay.Sender
					case "unfinalized refund":
						tx["finality"] = "pending"
					}
				}
				if change == "unrelated user transfer" && in["opcode"] == "0x0f8a7ea5" {
					in["value"] = "50000001"
				}
			}
			switch change {
			case "provisional trace":
				trace["is_incomplete"] = true
			case "no recipient block":
				e.Block = nil
			case "wrong master wallet":
				e.Wallet = json.RawMessage(`{}`)
			}
			e.Trace, _ = json.Marshal(index)
			if _, _, _, err := VerifyTONJettonDeposit(e, relay); err == nil {
				t.Fatal("credited incomplete or mismatched trace")
			}
		})
	}
}
