package chainreceipt

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const tonTestMaster = "kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy"

type tonFixture struct {
	Request  TONRequest
	Receipt  Receipt
	Evidence chainassets.TONEvidence
}
type tonFixtureSource struct {
	catalog []chainassets.Network
	proof   chainassets.TONEvidence
	calls   int
	err     error
}

func (s *tonFixtureSource) Catalog() []chainassets.Network { return s.catalog }
func (s *tonFixtureSource) ReadTONEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error) {
	s.calls++
	return s.proof, s.err
}
func tonSample(t *testing.T, name string) (tonFixture, *tonFixtureSource) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f tonFixture
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	a, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, URL: "http://127.0.0.1", TestUSDTContract: tonTestMaster}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	return f, &tonFixtureSource{catalog: a.Catalog(), proof: f.Evidence}
}
func tonTx(m map[string]any) map[string]any { return m["transactions"].([]any)[0].(map[string]any) }
func editTON(raw *json.RawMessage, edit func(map[string]any)) {
	mutate(raw, func(m map[string]any) { edit(tonTx(m)) })
}
func replaceTONIncoming(e *chainassets.TONEvidence, edit func(map[string]any)) {
	var message map[string]any
	editTON(&e.Transaction, func(tx map[string]any) { message = tx["in_msg"].(map[string]any); edit(message) })
	editTON(&e.Parent, func(tx map[string]any) {
		for i, m := range tx["out_msgs"].([]any) {
			if m.(map[string]any)["hash"] == message["hash"] {
				tx["out_msgs"].([]any)[i] = message
			}
		}
	})
}
func setTONIncomingBody(e *chainassets.TONEvidence, c *cell.Cell) {
	replaceTONIncoming(e, func(m map[string]any) {
		m["message_content"] = map[string]any{"body": base64.StdEncoding.EncodeToString(c.ToBOC()), "hash": base64.StdEncoding.EncodeToString(c.Hash())}
	})
}
func TestTONHistoricalReceiptsAndCanonicalIdentity(t *testing.T) {
	for _, name := range []string{"ton-native", "ton-jetton", "ton-no-state"} {
		t.Run(name, func(t *testing.T) {
			f, s := tonSample(t, name)
			b, _ := hex.DecodeString(f.Request.Hash)
			f.Request.Hash = base64.StdEncoding.EncodeToString(b)
			got, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || got.Transaction != f.Receipt.Transaction || got.Event != f.Receipt.Event {
				t.Fatal(got, err)
			}
			mutate(&s.proof.Masterchain, func(m map[string]any) { m["result"].(map[string]any)["last"].(map[string]any)["seqno"] = 999999999 })
			again, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || again.Transaction != got.Transaction {
				t.Fatal("head advancement changed receipt", err)
			}
		})
	}
}
func TestTONRejectsInputBeforeRPC(t *testing.T) {
	for _, edit := range []func(*TONRequest){func(r *TONRequest) { r.Hash = "fake" }, func(r *TONRequest) { r.Recipient = "fake" }, func(r *TONRequest) { r.Network = chainassets.TONMainnet }, func(r *TONRequest) { r.Atoms = "01" }, func(r *TONRequest) { r.Atoms = "0" }, func(r *TONRequest) { r.Atoms = "1.1" }, func(r *TONRequest) { r.Atoms = new(big.Int).Lsh(big.NewInt(1), 120).String() }, func(r *TONRequest) { r.AssetID = "ton-testnet:usdt" }} {
		f, s := tonSample(t, "ton-jetton")
		edit(&f.Request)
		_, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
		if !errors.Is(err, ErrInvalidRequest) || s.calls != 0 {
			t.Fatal(err, s.calls)
		}
	}
}
func TestTONRejectsUnconfirmedFailedAndForgedEvidence(t *testing.T) {
	cases := []struct {
		name string
		edit func(*chainassets.TONEvidence)
		want error
	}{
		{"absent", func(e *chainassets.TONEvidence) { e.Transaction = json.RawMessage(`{"transactions":[]}`) }, ErrUnconfirmed},
		{"null list", func(e *chainassets.TONEvidence) { e.Transaction = json.RawMessage(`{"transactions":null}`) }, ErrInvalidEvidence},
		{"wrong network", func(e *chainassets.TONEvidence) {
			mutate(&e.Masterchain, func(m map[string]any) {
				m["result"].(map[string]any)["init"].(map[string]any)["root_hash"] = chainassets.TONMainnetZeroState
			})
		}, ErrInvalidEvidence},
		{"future master", func(e *chainassets.TONEvidence) {
			mutate(&e.Masterchain, func(m map[string]any) { m["result"].(map[string]any)["last"].(map[string]any)["seqno"] = 1 })
		}, ErrUnconfirmed},
		{"pending", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["finality"] = "pending" })
		}, ErrUnconfirmed},
		{"emulated", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["emulated"] = true })
		}, ErrUnconfirmed},
		{"missing finality", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { delete(tx, "finality") })
		}, ErrUnconfirmed},
		{"wrong hash", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["hash"] = strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"wrong trace", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) { tx["trace_id"] = strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"absent parent", func(e *chainassets.TONEvidence) { e.Parent = json.RawMessage(`{"transactions":[]}`) }, ErrUnconfirmed},
		{"no sent message", func(e *chainassets.TONEvidence) {
			editTON(&e.Parent, func(tx map[string]any) {
				tx["out_msgs"] = []any{}
				tx["description"].(map[string]any)["action"].(map[string]any)["msgs_created"] = 0
			})
		}, ErrInvalidEvidence},
		{"message differs", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["in_msg"].(map[string]any)["value"] = "1" })
		}, ErrInvalidEvidence},
		{"bounced", func(e *chainassets.TONEvidence) {
			replaceTONIncoming(e, func(m map[string]any) { m["bounced"] = true })
		}, ErrInvalidEvidence},
		{"bad LT", func(e *chainassets.TONEvidence) { editTON(&e.Transaction, func(tx map[string]any) { tx["lt"] = "1" }) }, ErrInvalidEvidence},
		{"wrong block", func(e *chainassets.TONEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["seqno"] = 1 })
		}, ErrInvalidEvidence},
		{"wrong global ID", func(e *chainassets.TONEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["global_id"] = -239 })
		}, ErrInvalidEvidence},
		{"wrong MC anchor", func(e *chainassets.TONEvidence) {
			mutate(&e.Block, func(m map[string]any) {
				m["blocks"].([]any)[0].(map[string]any)["masterchain_block_ref"].(map[string]any)["seqno"] = 1
			})
		}, ErrInvalidEvidence},
		{"LT outside block", func(e *chainassets.TONEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["end_lt"] = "1" })
		}, ErrInvalidEvidence},
		{"abort", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) { tx["description"].(map[string]any)["aborted"] = true })
		}, ErrExecutionFailed},
		{"VM failure", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) {
				tx["description"].(map[string]any)["compute_ph"].(map[string]any)["exit_code"] = 33
			})
		}, ErrExecutionFailed},
		{"action failure", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) {
				tx["description"].(map[string]any)["action"].(map[string]any)["success"] = false
			})
		}, ErrExecutionFailed},
		{"skipped action", func(e *chainassets.TONEvidence) {
			editTON(&e.Transaction, func(tx map[string]any) {
				tx["description"].(map[string]any)["action"].(map[string]any)["skipped_actions"] = 1
			})
		}, ErrExecutionFailed},
		{"bad BoC", func(e *chainassets.TONEvidence) {
			replaceTONIncoming(e, func(m map[string]any) { m["message_content"].(map[string]any)["body"] = "bad" })
		}, ErrInvalidEvidence},
		{"wrong body hash", func(e *chainassets.TONEvidence) {
			replaceTONIncoming(e, func(m map[string]any) { m["message_content"].(map[string]any)["hash"] = strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"fake wallet", func(e *chainassets.TONEvidence) {
			mutate(&e.Wallet, func(m map[string]any) {
				m["stack"].([]any)[0].(map[string]any)["value"] = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreAddr(address.MustParseRawAddr("0:" + strings.Repeat("a", 64))).EndCell().ToBOC())
			})
		}, ErrInvalidEvidence},
		{"get method failure", func(e *chainassets.TONEvidence) { mutate(&e.Wallet, func(m map[string]any) { m["exit_code"] = 1 }) }, ErrInvalidEvidence},
		{"missing get method", func(e *chainassets.TONEvidence) { e.Wallet = json.RawMessage(`null`) }, ErrInvalidEvidence},
		{"forged notification", func(e *chainassets.TONEvidence) {
			c, err := tlb.ToCell(jetton.TransferNotification{QueryID: 2026110301, Amount: tlb.MustFromNano(big.NewInt(1000000), 6), Sender: address.MustParseRawAddr("0:" + strings.Repeat("a", 64)), ForwardPayload: cell.BeginCell().EndCell()})
			if err != nil {
				panic(err)
			}
			setTONIncomingBody(e, c)
		}, ErrInvalidEvidence},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f, s := tonSample(t, "ton-jetton")
			tt.edit(&s.proof)
			_, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
	for _, value := range []any{nil, "-1", "1.5", "01", 1.5} {
		f, s := tonSample(t, "ton-jetton")
		editTON(&s.proof.Transaction, func(tx map[string]any) { tx["total_fees"] = value })
		if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(value, err)
		}
	}
}
func TestTONNativeSkippedComputeAndComment(t *testing.T) {
	f, s := tonSample(t, "ton-no-state")
	editTON(&s.proof.Transaction, func(tx map[string]any) {
		tx["end_status"] = "active"
		tx["description"].(map[string]any)["compute_ph"].(map[string]any)["reason"] = "no_gas"
	})
	if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); err != nil {
		t.Fatal(err)
	}
	replaceTONIncoming(&s.proof, func(m map[string]any) { m["bounce"] = true })
	if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrExecutionFailed) {
		t.Fatal(err)
	}
	f, s = tonSample(t, "ton-native")
	c := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("order-public-reference").EndCell()
	setTONIncomingBody(&s.proof, c)
	if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); err != nil {
		t.Fatal(err)
	}
	editTON(&s.proof.Transaction, func(tx map[string]any) {
		tx["description"].(map[string]any)["credit_ph"].(map[string]any)["credit"] = "1"
	})
	if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal(err)
	}
}
func TestTONJettonAmountsAndOwnerComeFromBoC(t *testing.T) {
	f, s := tonSample(t, "ton-jetton")
	var tx tonTransaction
	tx, _ = tonOneTransaction(s.proof.Transaction)
	c, _ := tonBody(tx.In)
	var p tonInternalTransfer
	if err := tonPayload(c, &p); err != nil {
		t.Fatal(err)
	}
	p.Amount = tlb.MustFromNano(big.NewInt(1000001), 6)
	changed, err := tlb.ToCell(p)
	if err != nil {
		t.Fatal(err)
	}
	setTONIncomingBody(&s.proof, changed)
	if _, err := NewTONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal(err)
	}
	f, s = tonSample(t, "ton-jetton")
	replaceTONIncoming(&s.proof, func(m map[string]any) {
		m["message_content"].(map[string]any)["decoded"] = map[string]any{"amount": "999999", "sender": "fake"}
	})
	v, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Event.Atoms != "1000000" || v.Transaction.FeeAtoms != "849136" {
		t.Fatal(v, err)
	}
}
func TestTONRPCFailureStaysAnError(t *testing.T) {
	f, s := tonSample(t, "ton-native")
	s.err = errors.New("unavailable")
	_, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
	if !errors.Is(err, s.err) || errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
}
