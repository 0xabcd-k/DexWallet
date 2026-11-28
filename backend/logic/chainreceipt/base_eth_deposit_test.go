package chainreceipt

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type ethDepositSample struct {
	Quote                       chainassets.DepositPreview
	Wire, UserSignature, Digest string
	Evidence                    chainassets.BaseEvidence
}

func loadETHDeposit(t *testing.T) (ethDepositSample, *types.Transaction) {
	t.Helper()
	var f ethDepositSample
	raw, err := os.ReadFile("testdata/base-eth-self-deposit.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var tx types.Transaction
	if err = tx.UnmarshalBinary(hexutil.MustDecode(f.Wire)); err != nil {
		t.Fatal(err)
	}
	return f, &tx
}
func TestBaseETHDepositFinalHistory(t *testing.T) {
	f, tx := loadETHDeposit(t)
	a, err := f.Quote.BaseAuthorization()
	if err != nil || a.Digest().Hex() != f.Digest {
		t.Fatal("typed authorization", err)
	}
	got, err := VerifyBaseDeposit(f.Evidence, tx, f.Quote, f.UserSignature)
	if err != nil || got.Event.Kind != EVMCall || got.Event.Atoms != "7483510032780" || got.Transaction.FeeAtoms != "1969339155303" || got.Transaction.Slot != 64667583 {
		t.Fatal(got.Event, got.Transaction, err)
	}
}
func TestBaseETHDepositRejectsMismatchedEvidence(t *testing.T) {
	f, tx := loadETHDeposit(t)
	for _, field := range []string{"salt", "quantity", "start", "expiry", "asset", "sponsor", "absent trace", "extra payment", "payee forwards", "changed user", "changed recipient", "grant terms", "missing grant", "multiple grants", "pending", "failed"} {
		t.Run(field, func(t *testing.T) {
			q, e := f.Quote, f.Evidence
			// Model a finalized inclusion block for each mutation independently of the moving head.
			e.Finalized = e.Block
			var receipt map[string]any
			if err := json.Unmarshal(e.Receipt, &receipt); err != nil {
				t.Fatal(err)
			}
			var root baseCall
			if err := json.Unmarshal(e.CallTrace, &root); err != nil {
				t.Fatal(err)
			}
			logs := receipt["logs"].([]any)
			log := logs[len(logs)-1].(map[string]any)
			want := error(nil)
			switch field {
			case "salt":
				q.QuoteIdx = strings.Repeat("1", 32)
			case "quantity":
				q.PayQuantity = "0.00001"
			case "start":
				q.QuotedAt -= 2000
			case "expiry":
				q.BaseValidBefore++
			case "asset":
				q.AssetID = "base-sepolia:usdc"
			case "sponsor":
				q.FeePayer = "0x" + strings.Repeat("1", 40)
			case "absent trace":
				e.CallTrace = nil
			case "extra payment", "payee forwards", "changed user", "changed recipient":
				var leaf *baseCall
				var find func(*baseCall)
				find = func(c *baseCall) {
					if strings.EqualFold(c.From, q.WalletAddress) && strings.EqualFold(c.To, q.Recipient) && c.Value != "0x0" {
						leaf = c
					}
					for i := range c.Calls {
						find(&c.Calls[i])
					}
				}
				find(&root)
				if leaf == nil {
					t.Fatal("missing payment frame")
				}
				switch field {
				case "extra payment":
					root.Calls = append(root.Calls, *leaf)
				case "payee forwards":
					child := *leaf
					child.Type = "SELFDESTRUCT"
					child.From = leaf.To
					child.To = leaf.From
					leaf.Calls = []baseCall{child}
				case "changed user":
					leaf.From = "0x" + strings.Repeat("1", 40)
				case "changed recipient":
					leaf.To = "0x" + strings.Repeat("1", 40)
				}
				e.CallTrace, _ = json.Marshal(root)
			case "grant terms":
				log["data"] = "0x" + strings.Repeat("00", len(log["data"].(string))/2-1)
			case "missing grant":
				receipt["logs"] = logs[:len(logs)-1]
			case "multiple grants":
				clone := map[string]any{}
				for k, v := range log {
					clone[k] = v
				}
				clone["logIndex"] = "0xffff"
				receipt["logs"] = append(logs, clone)
			case "pending":
				e.Finalized = json.RawMessage(`{"hash":"0x` + strings.Repeat("1", 64) + `","number":"0x1"}`)
				want = ErrUnconfirmed
			case "failed":
				receipt["status"] = "0x0"
				e.CallTrace = nil
				want = ErrExecutionFailed
			}
			e.Receipt, _ = json.Marshal(receipt)
			_, err := VerifyBaseDeposit(e, tx, q, f.UserSignature)
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatal("bad proof accepted or wrong outcome", err)
			}
		})
	}
}
