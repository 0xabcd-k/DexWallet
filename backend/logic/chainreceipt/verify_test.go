package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/mr-tron/base58"
)

const sender = "9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf"
const receiver = "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"
const payer = "DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN"
const usdc = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
const sourceAccount = "G3fPnGibrCdEUDURrYFxk8Y1KfUmNWGPFsobTCRRbAdt"
const destinationAccount = "DT3EUVqAxQ4ZL6bvo419FiA9vpd4efKsn6Htmt4SLE5J"

var signature = base58.Encode(append([]byte{1}, make([]byte, 63)...))

type fixtureSource struct {
	catalog []chainassets.Network
	proof   chainassets.SolanaEvidence
	err     error
	calls   int
}

func (f *fixtureSource) Catalog() []chainassets.Network { return f.catalog }
func (f *fixtureSource) ReadSolanaEvidence(context.Context, string, string) (chainassets.SolanaEvidence, error) {
	f.calls++
	return f.proof, f.err
}
func fixture(t *testing.T, token bool) (Request, *fixtureSource) {
	t.Helper()
	src, err := chainassets.New(chainassets.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	r := Request{Network: chainassets.SolanaDevnet, Signature: signature, Instruction: 0, AssetID: "solana-devnet:sol", Recipient: receiver, Atoms: "1000"}
	keys := []map[string]any{{"pubkey": payer, "signer": true, "writable": true}, {"pubkey": sender, "signer": true, "writable": true}, {"pubkey": receiver, "signer": false, "writable": true}, {"pubkey": SystemProgram, "signer": false, "writable": false}}
	info := map[string]any{"source": sender, "destination": receiver, "lamports": 1000}
	program, kind := SystemProgram, "transfer"
	meta := map[string]any{"err": nil, "fee": 10000, "preTokenBalances": []any{}, "postTokenBalances": []any{}}
	if token {
		r.AssetID = "solana-devnet:usdc"
		r.Atoms = "10000"
		program, kind = TokenProgram, "transferChecked"
		keys = append(keys, map[string]any{"pubkey": sourceAccount, "signer": false, "writable": true}, map[string]any{"pubkey": destinationAccount, "signer": false, "writable": true}, map[string]any{"pubkey": TokenProgram, "signer": false, "writable": false}, map[string]any{"pubkey": usdc, "signer": false, "writable": false})
		info = map[string]any{"source": sourceAccount, "destination": destinationAccount, "authority": sender, "mint": usdc, "tokenAmount": map[string]any{"amount": "10000", "decimals": 6}}
		meta["preTokenBalances"] = []any{map[string]any{"accountIndex": 4, "owner": sender, "programId": TokenProgram, "mint": usdc, "uiTokenAmount": map[string]any{"amount": "20000000", "decimals": 6}}}
		meta["postTokenBalances"] = []any{map[string]any{"accountIndex": 5, "owner": receiver, "programId": TokenProgram, "mint": usdc, "uiTokenAmount": map[string]any{"amount": "10000", "decimals": 6}}}
	}
	tx := map[string]any{"slot": 123, "version": "legacy", "transaction": map[string]any{"signatures": []string{signature, signature}, "message": map[string]any{"accountKeys": keys, "instructions": []any{map[string]any{"programId": program, "parsed": map[string]any{"type": kind, "info": info}}}}}, "meta": meta}
	txRaw, _ := json.Marshal(tx)
	status := json.RawMessage(`{"context":{"slot":124},"value":[{"slot":123,"err":null,"confirmationStatus":"finalized"}]}`)
	return r, &fixtureSource{catalog: src.Catalog(), proof: chainassets.SolanaEvidence{Transaction: txRaw, Status: status}}
}
func mutate(raw *json.RawMessage, edit func(map[string]any)) {
	var m map[string]any
	_ = json.Unmarshal(*raw, &m)
	edit(m)
	*raw, _ = json.Marshal(m)
}
func info(m map[string]any) map[string]any {
	return m["transaction"].(map[string]any)["message"].(map[string]any)["instructions"].([]any)[0].(map[string]any)["parsed"].(map[string]any)["info"].(map[string]any)
}
func TestExactNativeAndTokenReceipt(t *testing.T) {
	for _, token := range []bool{false, true} {
		r, src := fixture(t, token)
		v, err := NewVerifier(src).Verify(context.Background(), r)
		if err != nil || v.Event.Atoms != r.Atoms || v.Event.RecipientOwner != receiver || v.Transaction.FeeAtoms != "10000" {
			t.Fatal(v, err)
		}
	}
}
func TestLargeAtomicIntegerNeverUsesFloat(t *testing.T) {
	r, src := fixture(t, false)
	r.Atoms = "18446744073709551615"
	mutate(&src.proof.Transaction, func(m map[string]any) { info(m)["lamports"] = json.Number(r.Atoms) })
	v, err := NewVerifier(src).Verify(context.Background(), r)
	if err != nil || v.Event.Atoms != r.Atoms {
		t.Fatal(v, err)
	}
}
func TestInvalidRequestsDoNotQuery(t *testing.T) {
	for _, edit := range []func(*Request){func(r *Request) { r.Atoms = "01" }, func(r *Request) { r.Atoms = "1.1" }, func(r *Request) { r.Atoms = "18446744073709551616" }, func(r *Request) { r.Signature = "fake" }, func(r *Request) { r.Network = chainassets.SolanaMainnet }, func(r *Request) { r.Recipient = "wrong" }, func(r *Request) { r.Instruction = -1 }} {
		r, src := fixture(t, false)
		edit(&r)
		_, err := NewVerifier(src).Verify(context.Background(), r)
		if !errors.Is(err, ErrInvalidRequest) || src.calls != 0 {
			t.Fatal(err, src.calls)
		}
	}
}
func TestUnconfirmedFailedAndInconsistentEvidence(t *testing.T) {
	tests := []struct {
		name string
		edit func(*fixtureSource)
		want error
	}{
		{"missing", func(s *fixtureSource) { s.proof.Transaction = json.RawMessage(`null`) }, ErrUnconfirmed},
		{"confirmed", func(s *fixtureSource) {
			s.proof.Status = json.RawMessage(`{"context":{"slot":124},"value":[{"slot":123,"err":null,"confirmationStatus":"confirmed"}]}`)
		}, ErrUnconfirmed},
		{"status absent", func(s *fixtureSource) { s.proof.Status = json.RawMessage(`{"context":{"slot":124},"value":[null]}`) }, ErrUnconfirmed},
		{"wrong slot", func(s *fixtureSource) {
			s.proof.Status = json.RawMessage(`{"context":{"slot":124},"value":[{"slot":122,"err":null,"confirmationStatus":"finalized"}]}`)
		}, ErrInvalidEvidence},
		{"meta absent", func(s *fixtureSource) { mutate(&s.proof.Transaction, func(m map[string]any) { m["meta"] = nil }) }, ErrInvalidEvidence},
		{"missing err", func(s *fixtureSource) {
			mutate(&s.proof.Transaction, func(m map[string]any) { delete(m["meta"].(map[string]any), "err") })
		}, ErrInvalidEvidence},
		{"wrong signature", func(s *fixtureSource) {
			mutate(&s.proof.Transaction, func(m map[string]any) { m["transaction"].(map[string]any)["signatures"] = []string{"fake"} })
		}, ErrInvalidEvidence},
		{"failed", func(s *fixtureSource) {
			mutate(&s.proof.Transaction, func(m map[string]any) {
				m["meta"].(map[string]any)["err"] = map[string]any{"InstructionError": []any{0, "error"}}
			})
			s.proof.Status = json.RawMessage(`{"context":{"slot":124},"value":[{"slot":123,"err":{"InstructionError":[0,"error"]},"confirmationStatus":"finalized"}]}`)
		}, ErrExecutionFailed},
		{"malformed execution error", func(s *fixtureSource) {
			mutate(&s.proof.Transaction, func(m map[string]any) { m["meta"].(map[string]any)["err"] = false })
			s.proof.Status = json.RawMessage(`{"context":{"slot":124},"value":[{"slot":123,"err":false,"confirmationStatus":"finalized"}]}`)
		}, ErrInvalidEvidence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, src := fixture(t, false)
			tt.edit(src)
			_, err := NewVerifier(src).Verify(context.Background(), r)
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
}
func TestWrongTransferCannotBeClaimed(t *testing.T) {
	edits := map[string]func(map[string]any){
		"amount":    func(m map[string]any) { info(m)["tokenAmount"].(map[string]any)["amount"] = "9999" },
		"mint":      func(m map[string]any) { info(m)["mint"] = payer },
		"decimals":  func(m map[string]any) { info(m)["tokenAmount"].(map[string]any)["decimals"] = 9 },
		"authority": func(m map[string]any) { info(m)["authority"] = receiver },
		"owner": func(m map[string]any) {
			m["meta"].(map[string]any)["postTokenBalances"].([]any)[0].(map[string]any)["owner"] = payer
		},
		"token program": func(m map[string]any) {
			m["meta"].(map[string]any)["postTokenBalances"].([]any)[0].(map[string]any)["programId"] = SystemProgram
		},
		"duplicate token index": func(m map[string]any) {
			m["meta"].(map[string]any)["postTokenBalances"] = append(m["meta"].(map[string]any)["postTokenBalances"].([]any), m["meta"].(map[string]any)["postTokenBalances"].([]any)[0])
		},
		"missing token index": func(m map[string]any) {
			delete(m["meta"].(map[string]any)["postTokenBalances"].([]any)[0].(map[string]any), "accountIndex")
		},
		"missing historical owner": func(m map[string]any) { m["meta"].(map[string]any)["postTokenBalances"] = []any{} },
		"CPI only": func(m map[string]any) {
			message := m["transaction"].(map[string]any)["message"].(map[string]any)
			m["meta"].(map[string]any)["innerInstructions"] = message["instructions"]
			message["instructions"] = []any{}
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			r, src := fixture(t, true)
			mutate(&src.proof.Transaction, edit)
			if _, err := NewVerifier(src).Verify(context.Background(), r); err == nil {
				t.Fatal("accepted bad transfer")
			}
		})
	}
	r, src := fixture(t, false)
	r.Recipient = sender
	if _, err := NewVerifier(src).Verify(context.Background(), r); err == nil {
		t.Fatal("wrong recipient")
	}
	r, src = fixture(t, true)
	src.err = errors.New("RPC offline")
	if _, err := NewVerifier(src).Verify(context.Background(), r); !errors.Is(err, src.err) {
		t.Fatal(err)
	}
}

// Public Devnet snapshots also run offline in the ordinary test suite. They
// exercise the actual RPC shape, including ATA creation before SPL transfers.
func TestHistoricalReceiptSnapshots(t *testing.T) {
	for _, name := range []string{"sol", "usdc", "usdt-fixture"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var sample struct {
				Request  Request
				Receipt  Receipt
				Evidence chainassets.SolanaEvidence
			}
			if err := json.Unmarshal(raw, &sample); err != nil {
				t.Fatal(err)
			}
			assets, err := chainassets.New(chainassets.Config{Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: "http://127.0.0.1", TestUSDTContract: "2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer assets.Close()
			src := &fixtureSource{catalog: assets.Catalog(), proof: sample.Evidence}
			actual, err := NewVerifier(src).Verify(context.Background(), sample.Request)
			if err != nil || actual.Transaction != sample.Receipt.Transaction || actual.Event != sample.Receipt.Event {
				t.Fatalf("snapshot %s differs: %+v, %v", name, actual, err)
			}
		})
	}
}
