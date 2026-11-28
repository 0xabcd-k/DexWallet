package chainassets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/mr-tron/base58"
)

// Replay signed orders for both Devnet deposit tokens independently of live RPC.
func TestSelfDepositSPL(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		assetID string
		atoms   uint64
		funding int64
	}{
		{"usdc-self-deposit-create", SolanaDevnetUSDC, 11000, 1488440},
		{"usdc-self-deposit-existing", SolanaDevnetUSDC, 11000, 0},
		{"test-usdt-self-deposit-create", SolanaDevnetTestUSDTPrefix + depositTestUSDTMint, 11225, 1488440},
		{"test-usdt-self-deposit-existing", SolanaDevnetTestUSDTPrefix + depositTestUSDTMint, 11225, 0},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			var f struct {
				Quote       DepositPreview
				Signature   string
				Transaction struct {
					Slot int64
					Meta struct {
						Err                                 json.RawMessage
						Fee                                 int64
						PreBalances, PostBalances           []uint64
						PreTokenBalances, PostTokenBalances []solanaspl.TokenBalance
					}
					Transaction []string
				}
			}
			raw, err := os.ReadFile("testdata/" + tc.fixture + ".json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			q := f.Quote
			wire, err := base64.StdEncoding.DecodeString(f.Transaction.Transaction[0])
			if err != nil {
				t.Fatal(err)
			}
			sig := base58.Encode(wire[65:129])
			terms := SolDepositTerms{Recipient: q.Recipient, TokenAssetID: tc.assetID, TokenAtoms: tc.atoms, FeeLamports: 10000, MaxFundingLamports: tc.funding}
			msg, err := terms.Check(q, q.FeePayer, sig)
			if err != nil || !bytes.Equal(msg, wire[129:]) {
				t.Fatal("signed terms", err)
			}
			net, err := q.FinalizedSolUSD(10000, tc.funding, sig)
			if err != nil || net.StringFixed(2) != "0.01" {
				t.Fatal("locked net excluding storage", net, err)
			}
			if _, err = q.FinalizedSolUSD(10000, tc.funding+1, sig); err == nil {
				t.Fatal("unapproved funding")
			}
			changed := terms
			changed.TokenAtoms++
			if _, err = changed.Check(q, q.FeePayer, sig); err == nil {
				t.Fatal("unapproved gross amount")
			}

			for _, otherID := range []string{SolanaDevnetTestUSDTPrefix + devnetUSDCMint, SolanaDevnetTestUSDTPrefix + q.WalletAddress, SolanaDevnetUSDC} {
				if otherID == tc.assetID {
					continue
				}
				wrong := terms
				wrong.TokenAssetID = otherID
				if _, err := wrong.Check(q, q.FeePayer, sig); err == nil {
					t.Fatal("order accepted for a different sponsored asset")
				}
				altered := q
				altered.AssetID = otherID
				if _, err := wrong.Check(altered, q.FeePayer, sig); err == nil {
					t.Fatal("asset substitution kept the original signature")
				}
			}

			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, _ := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getSignatureStatuses":
					rpcResult(w, map[string]any{"context": map[string]int64{"slot": f.Transaction.Slot}, "value": []any{map[string]any{"slot": f.Transaction.Slot, "err": f.Transaction.Meta.Err, "confirmationStatus": "finalized"}}})
				case "getTransaction":
					rpcResult(w, f.Transaction)
				default:
					t.Error(method)
				}
			})
			check := func(funding int64, failed bool) {
				t.Helper()
				o, err := s.ObserveSolDeposit(context.Background(), wire, terms)
				if err != nil || !o.Finalized || o.Failed != failed || o.Funding != funding {
					t.Fatal("finalized evidence", o, err)
				}
			}
			reject := func() {
				t.Helper()
				if _, err := s.ObserveSolDeposit(context.Background(), wire, terms); err == nil {
					t.Fatal("inconsistent receipt accepted")
				}
			}
			check(tc.funding, false)
			m := &f.Transaction.Meta
			m.PostBalances[0]--
			reject()
			m.PostBalances[0]++
			last := len(m.PostTokenBalances) - 1
			orig := m.PostTokenBalances[last]
			m.PostTokenBalances[last].Mint = q.Recipient
			reject()
			m.PostTokenBalances[last] = orig
			m.PostTokenBalances[last].UITokenAmount.Amount = "0"
			reject()
			m.PostTokenBalances[last] = orig
			if tc.funding > 0 {
				// Another transaction created this ATA after pricing. The idempotent
				// creation in our signed wire can legitimately spend zero storage SOL.
				m.PreBalances[2] = m.PostBalances[2]
				m.PostBalances[0] += uint64(tc.funding)
				for _, b := range m.PostTokenBalances {
					if *b.AccountIndex == 2 {
						b.UITokenAmount.Amount = "0"
						m.PreTokenBalances = append(m.PreTokenBalances, b)
					}
				}
				check(0, false)
			}
			m.Err = json.RawMessage(`{"InstructionError":[0,"InvalidAccountData"]}`)
			reject()
			m.PostBalances = append([]uint64(nil), m.PreBalances...)
			m.PostBalances[0] -= 10000
			m.PostTokenBalances = m.PreTokenBalances
			check(0, true)
		})
	}
}

func TestUSDCStorageBudgetBeforeSigningAndBroadcast(t *testing.T) {
	const authorized = int64(1488440)
	var current int64
	s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
		method, _ := rpcInput(t, r)
		switch method {
		case "getGenesisHash":
			rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
		case "getFeeForMessage":
			rpcResult(w, map[string]any{"context": map[string]int{"slot": 12}, "value": 10000})
		case "getMinimumBalanceForRentExemption":
			rpcResult(w, current)
		default:
			t.Error(method)
		}
	})
	terms := SolDepositTerms{TokenAssetID: SolanaDevnetUSDC, TokenAtoms: 11000, FeeLamports: 10000, MaxFundingLamports: authorized}
	for _, cost := range []int64{authorized, authorized + 1, 0} {
		current = cost
		err := s.CheckDevnetDepositFee(context.Background(), []byte{1}, terms)
		if (err == nil) != (cost == authorized) {
			t.Fatalf("current storage %d, authorized %d: %v", cost, authorized, err)
		}
	}
}
