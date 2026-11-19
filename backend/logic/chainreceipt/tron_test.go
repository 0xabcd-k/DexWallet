package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type tronFixture struct {
	Request  TRONRequest
	Receipt  Receipt
	Evidence chainassets.TRONEvidence
}
type tronFixtureSource struct {
	catalog []chainassets.Network
	proof   chainassets.TRONEvidence
	calls   int
	err     error
}

func (s *tronFixtureSource) Catalog() []chainassets.Network { return s.catalog }
func (s *tronFixtureSource) ReadTRONEvidence(context.Context, string, string) (chainassets.TRONEvidence, error) {
	s.calls++
	return s.proof, s.err
}
func tronSample(t *testing.T, name string) (tronFixture, *tronFixtureSource) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f tronFixture
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	contract := ""
	if parts := strings.Split(f.Request.AssetID, ":"); len(parts) == 3 {
		contract = parts[2]
	}
	assets, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: "http://127.0.0.1", TestUSDTContract: contract}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	return f, &tronFixtureSource{catalog: assets.Catalog(), proof: f.Evidence}
}
func tronLog(m map[string]any) map[string]any { return m["log"].([]any)[0].(map[string]any) }
func TestTRONHistoricalReceipts(t *testing.T) {
	for _, name := range []string{"tron-trx", "tron-usdt", "tron-usdt-fixture"} {
		t.Run(name, func(t *testing.T) {
			f, s := tronSample(t, name)
			f.Request.Hash = strings.ToUpper(f.Request.Hash)
			got, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || got.Transaction != f.Receipt.Transaction || got.Event != f.Receipt.Event {
				t.Fatalf("%+v %v", got, err)
			}
			if got.Transaction.FeeAtoms != "0" {
				t.Fatal("historical zero burn misread")
			}
			// Proto3 omitted zero and explicit numeric zero describe the same facts.
			mutate(&s.proof.Info, func(m map[string]any) { m["fee"] = 0; m["receipt"].(map[string]any)["net_fee"] = 0 })
			again, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || again.Transaction != got.Transaction {
				t.Fatal("zero default changed identity", err)
			}
		})
	}
}
func TestTRONRejectsBadRequestsBeforeRPC(t *testing.T) {
	for _, edit := range []func(*TRONRequest){
		func(r *TRONRequest) { r.Hash = "0x" + r.Hash }, func(r *TRONRequest) { r.Recipient = "fake" }, func(r *TRONRequest) { r.Recipient = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb" }, func(r *TRONRequest) { r.Atoms = "01" }, func(r *TRONRequest) { r.Atoms = "0" }, func(r *TRONRequest) { r.Atoms = "1.1" }, func(r *TRONRequest) { r.Atoms = new(big.Int).Lsh(big.NewInt(1), 256).String() }, func(r *TRONRequest) { r.LogIndex = nil }, func(r *TRONRequest) { i := -1; r.LogIndex = &i }, func(r *TRONRequest) { r.AssetID = "tron-nile:usdt" }, func(r *TRONRequest) { r.Network = chainassets.TRONMainnet },
	} {
		f, s := tronSample(t, "tron-usdt")
		edit(&f.Request)
		_, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
		if !errors.Is(err, ErrInvalidRequest) || s.calls != 0 {
			t.Fatal(err, s.calls)
		}
	}
	f, s := tronSample(t, "tron-trx")
	f.Request.Atoms = "9223372036854775808"
	_, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
	if !errors.Is(err, ErrInvalidRequest) || s.calls != 0 {
		t.Fatal(err)
	}
}
func TestTRONRejectsUnconfirmedFailedAndMismatchedEvidence(t *testing.T) {
	cases := []struct {
		name string
		edit func(*chainassets.TRONEvidence)
		want error
	}{
		{"absent", func(e *chainassets.TRONEvidence) { e.Info = json.RawMessage(`{}`) }, ErrUnconfirmed},
		{"null", func(e *chainassets.TRONEvidence) { e.Info = json.RawMessage(`null`) }, ErrInvalidEvidence},
		{"error", func(e *chainassets.TRONEvidence) { e.Info = json.RawMessage(`{"Error":"failed"}`) }, ErrInvalidEvidence},
		{"wrong network", func(e *chainassets.TRONEvidence) {
			mutate(&e.Genesis, func(m map[string]any) { m["blockID"] = chainassets.TRONMainnetGenesis })
		}, ErrInvalidEvidence},
		{"missing genesis header", func(e *chainassets.TRONEvidence) {
			mutate(&e.Genesis, func(m map[string]any) { delete(m, "block_header") })
		}, ErrInvalidEvidence},
		{"not solid", func(e *chainassets.TRONEvidence) {
			mutate(&e.SolidHead, func(m map[string]any) {
				m["blockID"] = fmt.Sprintf("%016x%s", 1, strings.Repeat("a", 48))
				m["block_header"].(map[string]any)["raw_data"].(map[string]any)["number"] = 1
			})
		}, ErrUnconfirmed},
		{"no block", func(e *chainassets.TRONEvidence) { e.Block = json.RawMessage(`{}`) }, ErrUnconfirmed},
		{"wrong block height", func(e *chainassets.TRONEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["block_header"].(map[string]any)["raw_data"].(map[string]any)["number"] = 1 })
		}, ErrInvalidEvidence},
		{"wrong block time", func(e *chainassets.TRONEvidence) {
			mutate(&e.Block, func(m map[string]any) {
				m["block_header"].(map[string]any)["raw_data"].(map[string]any)["timestamp"] = 1
			})
		}, ErrInvalidEvidence},
		{"not in block", func(e *chainassets.TRONEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["transactions"] = []any{} })
		}, ErrInvalidEvidence},
		{"duplicate member", func(e *chainassets.TRONEvidence) {
			mutate(&e.Block, func(m map[string]any) {
				members := m["transactions"].([]any)
				for _, v := range members {
					if v.(map[string]any)["txID"] == "caff6737825af0e5dddb53d148336956a0a62c5a6fa67ff7385f2fe00c2b3006" {
						m["transactions"] = append(members, v)
						break
					}
				}
			})
		}, ErrInvalidEvidence},
		{"wrong raw hash", func(e *chainassets.TRONEvidence) {
			mutate(&e.Transaction, func(m map[string]any) { m["raw_data_hex"] = "00" })
		}, ErrInvalidEvidence},
		{"wrong receipt ID", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { m["id"] = strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"failed", func(e *chainassets.TRONEvidence) {
			mutate(&e.Transaction, func(m map[string]any) { m["ret"].([]any)[0].(map[string]any)["contractRet"] = "REVERT" })
			mutate(&e.Info, func(m map[string]any) { m["result"] = "FAILED"; m["receipt"].(map[string]any)["result"] = "REVERT" })
		}, ErrExecutionFailed},
		{"unknown status", func(e *chainassets.TRONEvidence) {
			mutate(&e.Transaction, func(m map[string]any) { m["ret"].([]any)[0].(map[string]any)["contractRet"] = "invented" })
		}, ErrInvalidEvidence},
		{"missing VM success", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { delete(m["receipt"].(map[string]any), "result") })
		}, ErrInvalidEvidence},
		{"null resource", func(e *chainassets.TRONEvidence) { mutate(&e.Info, func(m map[string]any) { m["receipt"] = nil }) }, ErrInvalidEvidence},
		{"invalid type URL", func(e *chainassets.TRONEvidence) {
			mutate(&e.Transaction, func(m map[string]any) {
				m["raw_data"].(map[string]any)["contract"].([]any)[0].(map[string]any)["parameter"].(map[string]any)["type_url"] = "other"
			})
		}, ErrInvalidEvidence},
		{"wrong contract", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["address"] = strings.Repeat("a", 40) })
		}, ErrInvalidEvidence},
		{"wrong topic", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["topics"].([]any)[0] = strings.Repeat("0", 64) })
		}, ErrInvalidEvidence},
		{"bad topic padding", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["topics"].([]any)[2] = strings.Repeat("1", 64) })
		}, ErrInvalidEvidence},
		{"wrong recipient", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) {
				tronLog(m)["topics"].([]any)[2] = strings.Repeat("0", 24) + strings.Repeat("a", 40)
			})
		}, ErrInvalidEvidence},
		{"wrong amount", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["data"] = strings.Repeat("0", 63) + "1" })
		}, ErrInvalidEvidence},
		{"mint", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["topics"].([]any)[1] = strings.Repeat("0", 64) })
		}, ErrInvalidEvidence},
		{"self", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { l := tronLog(m); l["topics"].([]any)[1] = l["topics"].([]any)[2] })
		}, ErrInvalidEvidence},
		{"short amount", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { tronLog(m)["data"] = "2710" })
		}, ErrInvalidEvidence},
		{"no logs", func(e *chainassets.TRONEvidence) { mutate(&e.Info, func(m map[string]any) { delete(m, "log") }) }, ErrUnsupported},
		{"fee smaller than burn", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) { m["receipt"].(map[string]any)["net_fee"] = 1 })
		}, ErrInvalidEvidence},
		{"resource overflow", func(e *chainassets.TRONEvidence) {
			mutate(&e.Info, func(m map[string]any) {
				m["receipt"].(map[string]any)["energy_usage"] = json.Number("9223372036854775807")
			})
		}, ErrInvalidEvidence},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f, s := tronSample(t, "tron-usdt")
			tt.edit(&s.proof)
			_, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
	for _, value := range []any{nil, -1, 1.5, "100", json.Number("9223372036854775808")} {
		f, s := tronSample(t, "tron-usdt")
		mutate(&s.proof.Info, func(m map[string]any) { m["fee"] = value })
		if _, err := NewTRONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(value, err)
		}
	}
}
func TestTRONLogPositionWideAmountAndBurnedFees(t *testing.T) {
	f, s := tronSample(t, "tron-usdt")
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	f.Request.Atoms = max.String()
	index := 1
	f.Request.LogIndex = &index
	mutate(&s.proof.Info, func(m map[string]any) {
		log := tronLog(m)
		log["data"] = strings.Repeat("f", 64)
		m["log"] = []any{map[string]any{"address": strings.Repeat("a", 40), "topics": []any{}, "data": ""}, log}
		m["fee"] = 1200
		m["receipt"].(map[string]any)["net_fee"] = 200
		m["receipt"].(map[string]any)["energy_fee"] = 1000
	})
	v, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Event.Instruction != 1 || v.Event.Atoms != max.String() || v.Transaction.FeeAtoms != "1200" {
		t.Fatal(v, err)
	}
	index = 0
	if _, err = NewTRONVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("logs were filtered before indexing", err)
	}
	f, s = tronSample(t, "tron-trx")
	mutate(&s.proof.Info, func(m map[string]any) { m["fee"] = 1100000; m["receipt"].(map[string]any)["net_fee"] = 100000 })
	v, err = NewTRONVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Transaction.FeeAtoms != "1100000" {
		t.Fatal("activation/other fees lost", err)
	}
}
func TestTRONRPCFailureIsNotMissingTransaction(t *testing.T) {
	f, s := tronSample(t, "tron-trx")
	s.err = errors.New("RPC unavailable")
	_, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
	if !errors.Is(err, s.err) || errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
}

func TestTRONNativeTransferAndTokenOwnerBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(map[string]any)
		want error
	}{
		{"zero", func(c map[string]any) { c["parameter"].(map[string]any)["value"].(map[string]any)["amount"] = 0 }, ErrInvalidEvidence},
		{"fraction", func(c map[string]any) { c["parameter"].(map[string]any)["value"].(map[string]any)["amount"] = 1.5 }, ErrInvalidEvidence},
		{"wrong address prefix", func(c map[string]any) {
			c["parameter"].(map[string]any)["value"].(map[string]any)["to_address"] = "40" + strings.Repeat("a", 40)
		}, ErrInvalidEvidence},
		{"internal native", func(c map[string]any) {
			c["type"] = tronTriggerContract
			c["parameter"].(map[string]any)["type_url"] = "type.googleapis.com/protocol." + tronTriggerContract
		}, ErrUnsupported},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, s := tronSample(t, "tron-trx")
			mutate(&s.proof.Transaction, func(m map[string]any) {
				tt.edit(m["raw_data"].(map[string]any)["contract"].([]any)[0].(map[string]any))
			})
			if tt.want == ErrUnsupported {
				mutate(&s.proof.Info, func(m map[string]any) { m["receipt"].(map[string]any)["result"] = "SUCCESS" })
			}
			_, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
	f, s := tronSample(t, "tron-usdt")
	mutate(&s.proof.Info, func(m map[string]any) {
		tronLog(m)["topics"].([]any)[1] = strings.Repeat("0", 24) + strings.Repeat("a", 40)
	})
	v, err := NewTRONVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Transaction.FeePayer == v.Event.SourceOwner {
		t.Fatal("transaction owner substituted for token sender", err)
	}
}
