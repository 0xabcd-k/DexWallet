package chainassets

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

const (
	devnetFeePayer         = "DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN"
	devnetDepositRecipient = "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"
	depositBlockhash       = "F1VvomZpGxaB9taEwSuT1WwAaqP99RYjQXoEkodmvxhA"
)

func solanaQuoteSheet() *DepositQuoteConfig {
	now := time.Now().Unix()
	return &DepositQuoteConfig{Version: "sol-test", AsOf: now - 60, ValidUntil: now + 60,
		SolanaSOL: &SolanaDepositFeeConfig{devnetFeePayer, devnetDepositRecipient},
		Assets:    []DepositQuoteRate{{solanaDepositAsset, "100", "0.001"}}}
}

func TestSolanaDepositUsesFeeOfFinalMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fees    []any
		success bool
	}{
		{"valid", []any{12345, 12345}, true}, {"unknown block", []any{nil}, false},
		{"changed", []any{12345, 12346}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages [][]byte
			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, params := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getLatestBlockhash":
					rpcResult(w, map[string]any{"value": SolanaBlock{depositBlockhash, 123456}})
				case "getFeeForMessage":
					var msg []byte // base64-decoded by encoding/json
					var options map[string]string
					if len(params) != 2 || json.Unmarshal(params[0], &msg) != nil || json.Unmarshal(params[1], &options) != nil || options["commitment"] != "confirmed" || len(msg) != 182 {
						t.Error("invalid fee request")
						return
					}
					if len(messages) >= len(tc.fees) {
						t.Error("unexpected fee query")
						return
					}
					fee := tc.fees[len(messages)]
					messages = append(messages, msg)
					rpcResult(w, map[string]any{"context": map[string]int{"slot": 100}, "value": fee})
				default:
					t.Error(method)
				}
			}, func(c *Config) { c.DepositQuotes = solanaQuoteSheet() })
			for _, wallet := range []string{"invalid", devnetFeePayer, devnetDepositRecipient} {
				if _, err := s.PreviewWalletDeposit(context.Background(), 7, solanaDepositAsset, "10", wallet); err == nil {
					t.Fatal(wallet)
				}
			}
			q, err := s.PreviewWalletDeposit(context.Background(), 7, solanaDepositAsset, "10", solOwner)
			if (err == nil) != tc.success || len(messages) != len(tc.fees) {
				t.Fatal(q, err, len(messages))
			}
			if !tc.success {
				if q.MessageSHA256 != "" || q.PayQuantity != "" {
					t.Fatal(q)
				}
				return
			}
			first, final := messages[0], messages[1]
			if !bytes.Equal(first[:len(first)-8], final[:len(final)-8]) || binary.LittleEndian.Uint64(first[len(first)-8:]) != 100000000 || binary.LittleEndian.Uint64(final[len(final)-8:]) != 100012345 {
				t.Fatal("incorrect gross lamports")
			}
			if !q.WalletFeePreview || q.WalletAddress != solOwner || q.MessageSHA256 != fmt.Sprintf("%x", sha256.Sum256(final)) || q.EstimatedGas != "0.000012345" || q.GasUSD != "0.0012345" || q.NetUSD != "10.00" || q.PayQuantity != "0.100012345" {
				t.Fatal(q)
			}
		})
	}
}

// Replay both fee requests from the live quote. The signed, finalized transaction
// must carry precisely the second message and debit exactly the quoted amounts.
func TestSolanaDepositMatchesFinalizedFeeEvidence(t *testing.T) {
	data, err := os.ReadFile("testdata/solana-devnet-deposit-fee.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Addresses struct{ User, Sponsor, Recipient string }
		Block     struct{ Value SolanaBlock }
		Fees      []struct {
			MessageBase64 []byte
			Response      struct{ Value uint64 }
		}
		Quote     DepositPreview
		Signature string
		Status    struct {
			ConfirmationStatus string
			Slot               uint64
		}
		Receipt struct {
			Slot        uint64
			Transaction []string
		}
		Meta struct {
			Fee                       uint64
			Err                       json.RawMessage
			PreBalances, PostBalances []int64
		}
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	route, err := newSolanaDepositRoute(SolanaDepositFeeConfig{f.Addresses.Sponsor, f.Addresses.Recipient})
	if err != nil {
		t.Fatal(err)
	}
	user, err := depositKey(f.Addresses.User, true)
	if err != nil {
		t.Fatal(err)
	}
	block, err := base58.Decode(f.Block.Value.Blockhash)
	if err != nil {
		t.Fatal(err)
	}
	total, err := decimal.NewFromString(f.Quote.PayQuantity)
	if err != nil {
		t.Fatal(err)
	}
	gross := total.Shift(9).BigInt().Uint64()
	msg := route.message(user, block, gross)
	if len(f.Fees) != 2 || gross <= f.Meta.Fee || f.Quote.WalletAddress != f.Addresses.User ||
		!bytes.Equal(f.Fees[0].MessageBase64, route.message(user, block, gross-f.Meta.Fee)) ||
		!bytes.Equal(f.Fees[1].MessageBase64, msg) || fmt.Sprintf("%x", sha256.Sum256(msg)) != f.Quote.MessageSHA256 {
		t.Fatal("priced messages differ from the deposit")
	}
	if len(f.Receipt.Transaction) != 2 || f.Receipt.Transaction[1] != "base64" {
		t.Fatal("expected base64 transaction")
	}
	wire, err := base64.StdEncoding.DecodeString(f.Receipt.Transaction[0])
	const messageOffset = 1 + 2*ed25519.SignatureSize
	if err != nil || len(wire) != messageOffset+len(msg) || wire[0] != 2 || !bytes.Equal(msg, wire[messageOffset:]) ||
		base58.Encode(wire[1:1+ed25519.SignatureSize]) != f.Signature {
		t.Fatal("finalized wire differs")
	}
	for i, key := range [][32]byte{route.payer, user} {
		start := 1 + i*ed25519.SignatureSize
		if !ed25519.Verify(key[:], msg, wire[start:start+ed25519.SignatureSize]) {
			t.Fatal("invalid chain signature", i)
		}
	}
	if f.Status.ConfirmationStatus != "finalized" || f.Receipt.Slot == 0 || f.Status.Slot != f.Receipt.Slot || string(f.Meta.Err) != "null" ||
		f.Meta.Fee == 0 || f.Meta.Fee != f.Fees[0].Response.Value || f.Meta.Fee != f.Fees[1].Response.Value {
		t.Fatal("fee/finality mismatch")
	}
	if len(f.Meta.PreBalances) != 4 || len(f.Meta.PostBalances) != 4 {
		t.Fatal("missing receipt balances")
	}
	for i, delta := range []int64{-int64(f.Meta.Fee), -int64(gross), int64(gross), 0} {
		if f.Meta.PostBalances[i]-f.Meta.PreBalances[i] != delta {
			t.Fatalf("wrong balance change for account %d", i)
		}
	}
}
