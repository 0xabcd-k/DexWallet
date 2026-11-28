package chainassets

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mr-tron/base58"
)

func TestSolDepositSponsorVerifiesSignedInstructions(t *testing.T) {
	source, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	route, err := newSolanaDepositRoute(SolanaDepositFeeConfig{devnetFeePayer, devnetDepositRecipient})
	if err != nil {
		t.Fatal(err)
	}
	terms := SolDepositTerms{Recipient: devnetDepositRecipient, GrossLamports: 110000, FeeLamports: 10000}
	order := strings.Repeat("c", 32)
	hash, _ := base58.Decode(depositBlockhash)
	msg := route.message([32]byte(source), hash, 110000, order)
	base := DepositPreview{SolOrderIdx: order, AssetID: solanaDepositAsset, GasAssetID: solanaDepositAsset, WalletFeePreview: true,
		FeePayer: devnetFeePayer, Recipient: devnetDepositRecipient, MaxGas: "0.00001", EstimatedGas: "0.00001", PayQuantity: "0.00011", WalletAddress: base58.Encode(source)}
	t.Run("beneficiary bound to the signed payment", func(t *testing.T) {
		q := base
		q.Beneficiary = &DepositBeneficiary{UserID: 2, Idx: "01234567890123456"}
		wire := route.message([32]byte(source), hash, 110000, solMemoReference(order, q.Beneficiary))
		q.SolMessage = base64.StdEncoding.EncodeToString(wire)
		q.MessageSHA256 = fmt.Sprintf("%x", sha256.Sum256(wire))
		sig := base58.Encode(ed25519.Sign(key, wire))
		if _, err := terms.Check(q, devnetFeePayer, sig); err != nil {
			t.Fatal(err)
		}
		for _, replacement := range []*DepositBeneficiary{nil, {UserID: 3, Idx: "09876543210987654"}} {
			q.Beneficiary = replacement
			if _, err := terms.Check(q, devnetFeePayer, sig); err == nil {
				t.Fatal("reassigned signed payment")
			}
		}
	})
	for _, tc := range []struct {
		name    string
		message []byte
		valid   bool
	}{
		{"authorized", msg, true},
		{"different amount", route.message([32]byte(source), hash, 110001, order), false},
		{"other order memo", route.message([32]byte(source), hash, 110000, strings.Repeat("b", 32)), false},
		{"extra instruction", append(append([]byte(nil), msg...), 0), false},
		{"truncated", msg[:64], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			q.SolMessage = base64.StdEncoding.EncodeToString(tc.message)
			q.MessageSHA256 = fmt.Sprintf("%x", sha256.Sum256(tc.message))
			sig := base58.Encode(ed25519.Sign(key, tc.message))
			if _, err := terms.Check(q, devnetFeePayer, sig); (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if _, err := terms.Check(q, devnetFeePayer, strings.Repeat("1", 64)); err == nil {
				t.Fatal("wrong user signature")
			}
		})
	}
	t.Run("credit in cents after subtracting the actual fee", func(t *testing.T) {
		q := base
		// 60,003 lamports gross at 199.99 USD/SOL, including 10,000 lamports Gas.
		// Truncating gross USD before deducting the fee would lose one cent.
		msg := route.message([32]byte(source), hash, 60003, order)
		sig := base58.Encode(ed25519.Sign(key, msg))
		q.SolMessage = base64.StdEncoding.EncodeToString(msg)
		q.MessageSHA256 = fmt.Sprintf("%x", sha256.Sum256(msg))
		q.PayQuantity, q.USDPerCoin, q.GasUSDPerCoin = "0.000060003", "199.99", "199.99"
		q.GasUSD, q.NetUSD = "0.0019999", "0.01"
		net, err := q.FinalizedSolUSD(10000, 0, sig)
		if err != nil || net.StringFixed(2) != "0.01" {
			t.Fatal(net, err)
		}
		if _, err = q.FinalizedSolUSD(10001, 0, sig); err == nil {
			t.Fatal("fee differs from quote")
		}
		q.NetUSD = "0.02"
		if _, err = q.FinalizedSolUSD(10000, 0, sig); err == nil {
			t.Fatal("net differs from quote")
		}
	})

}

func TestSolDepositFinalizedEvidence(t *testing.T) {
	wire := make([]byte, 160)
	wire[0] = 2
	encoded := base64.StdEncoding.EncodeToString(wire)
	terms := SolDepositTerms{Recipient: devnetDepositRecipient, GrossLamports: 110000, FeeLamports: 10000}
	for _, tc := range []struct {
		name, body    string
		post          []uint64
		fee           int64
		failed, valid bool
	}{
		{"success", encoded, []uint64{10000, 10000, 110001, 1, 1}, 10000, false, true},
		{"failed charges only sponsor", encoded, []uint64{10000, 120000, 1, 1, 1}, 10000, true, true},
		{"different signed transaction", "AAAA", []uint64{10000, 10000, 110001, 1, 1}, 10000, false, false},
		{"unexpected fee", encoded, []uint64{9999, 10000, 110001, 1, 1}, 10001, false, false},
		{"recipient did not receive gross", encoded, []uint64{10000, 10000, 110000, 1, 1}, 10000, false, false},
		{"failed but debited user", encoded, []uint64{10000, 10000, 110001, 1, 1}, 10000, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var outcome any
			if tc.failed {
				outcome = map[string]any{"InstructionError": []any{0, "InvalidAccountData"}}
			}
			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, params := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getSignatureStatuses":
					if len(params) != 2 || string(params[1]) != "{\"searchTransactionHistory\":true}" {
						t.Error("history lookup required")
					}
					rpcResult(w, map[string]any{"context": map[string]int{"slot": 8}, "value": []any{map[string]any{"slot": 7, "err": outcome, "confirmationStatus": "finalized"}}})
				case "getTransaction":
					rpcResult(w, map[string]any{"slot": 7, "transaction": []string{tc.body, "base64"}, "meta": map[string]any{"err": outcome, "fee": tc.fee, "preBalances": []uint64{20000, 120000, 1, 1, 1}, "postBalances": tc.post}})
				default:
					t.Error(method)
				}
			})
			obs, err := s.ObserveSolDeposit(context.Background(), wire, terms)
			if (err == nil) != tc.valid || obs.Finalized != tc.valid || tc.valid && (obs.Failed != tc.failed || obs.Fee != tc.fee || obs.Slot != 7) {
				t.Fatal(obs, err)
			}
		})
	}
}

func TestSolDepositReplayNeedsBothHistoriesAbsent(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status, transaction any
		seen                bool
	}{
		{"absent", nil, nil, false},
		{"archived transaction only", nil, map[string]any{"slot": 7}, true},
		{"finalized status only", map[string]any{"slot": 7, "err": nil, "confirmationStatus": "finalized"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, _ := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getSignatureStatuses":
					rpcResult(w, map[string]any{"context": map[string]int{"slot": 8}, "value": []any{tc.status}})
				case "getTransaction":
					rpcResult(w, tc.transaction)
				default:
					t.Error(method)
				}
			})
			wire := make([]byte, 160)
			wire[0] = 2
			obs, err := s.ObserveSolDeposit(context.Background(), wire, SolDepositTerms{Recipient: devnetDepositRecipient, GrossLamports: 110000, FeeLamports: 10000})
			if err != nil || obs.Seen != tc.seen || obs.Finalized {
				t.Fatal(obs, err)
			}
		})
	}
}
