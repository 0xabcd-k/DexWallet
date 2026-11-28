package chainassets

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/mr-tron/base58"
)

const depositTestUSDTMint = "2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"

func TestUSDCDepositFinalizedQuotes(t *testing.T) {
	checkTokenDepositReceipts(t, "usdc", SolanaDevnetUSDC, "1", "0.011")
}

func TestSolanaTestUSDTDepositFinalizedQuotes(t *testing.T) {
	checkTokenDepositReceipts(t, "usdt", SolanaDevnetTestUSDTPrefix+depositTestUSDTMint, "0.98", "0.011225")
}

// Replay both getFeeForMessage calls for each finalized transfer.
func checkTokenDepositReceipts(t *testing.T, token, assetID, rate, payQuantity string) {
	t.Helper()
	for _, tc := range []struct{ account, funding, fundingUSD string }{
		{"new", "0.00148844", "0.148844"}, {"existing", "0", "0"},
	} {
		t.Run(tc.account, func(t *testing.T) {
			data, err := os.ReadFile("testdata/solana-" + token + "-deposit-" + tc.account + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var f struct {
				Addresses                                     struct{ User, Sponsor, Recipient string }
				Block, AccountSnapshot                        json.RawMessage
				Genesis, Signature, SourceATA, DestinationATA string
				RentLamports                                  int64
				Fees                                          []struct {
					MessageBase64 string
					Response      json.RawMessage
				}
				Quote  DepositPreview
				Status struct {
					ConfirmationStatus string
					Slot               int64
				}
				Receipt struct {
					Slot        int64
					Transaction []string
				}
				Meta struct {
					Fee                       int64
					Err                       json.RawMessage
					PreBalances, PostBalances []int64
				}
			}
			if err = json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			fees, rents := 0, 0
			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, params := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, f.Genesis)
				case "getLatestBlockhash":
					rpcResult(w, f.Block)
				case "getMultipleAccounts":
					var addresses []string
					if len(params) != 2 || json.Unmarshal(params[0], &addresses) != nil || len(addresses) != 2 || addresses[0] != f.SourceATA || addresses[1] != f.DestinationATA {
						t.Error("incorrect token account selection")
						return
					}
					rpcResult(w, f.AccountSnapshot)
				case "getMinimumBalanceForRentExemption":
					rents++
					if tc.account != "new" || len(params) != 2 || string(params[0]) != "165" {
						t.Error("unexpected account funding query")
					}
					rpcResult(w, f.RentLamports)
				case "getFeeForMessage":
					var msg string
					if fees >= len(f.Fees) || len(params) != 2 || json.Unmarshal(params[0], &msg) != nil || msg != f.Fees[fees].MessageBase64 {
						t.Error("unexpected fee message")
						return
					}
					rpcResult(w, f.Fees[fees].Response)
					fees++
				default:
					t.Error(method)
				}
			}, func(c *Config) {
				c.DepositQuotes = solanaQuoteSheet()
				route := &SolanaDepositFeeConfig{f.Addresses.Sponsor, f.Addresses.Recipient}
				if assetID == SolanaDevnetUSDC {
					c.DepositQuotes.SolanaUSDC = route
				} else {
					c.Networks[0].TestUSDTContract = depositTestUSDTMint
					c.DepositQuotes.SolanaTestUSDT = route
					c.DepositQuotes.SolanaSOLReviewAfter = time.Minute // Native SOL enablement never enables the fixture.
				}
				c.DepositQuotes.Assets = append(c.DepositQuotes.Assets, DepositQuoteRate{assetID, rate, "0"})
			})
			q, err := s.PreviewWalletDeposit(context.Background(), 7, assetID, "0.01", f.Addresses.User)
			if err != nil || q.SolOrderIdx != "" || fees != 2 || (rents == 1) != (tc.account == "new") || q.EstimatedGas != "0.00001" || q.GasUSD != "0.001" ||
				q.AccountFunding != tc.funding || q.AccountFundingUSD != tc.fundingUSD || q.AssetID != assetID || q.USDPerCoin != rate || q.PayQuantity != payQuantity || q.NetUSD != "0.01" ||
				q.MessageSHA256 != f.Quote.MessageSHA256 || q.WalletAddress != f.Addresses.User {
				t.Fatal(q, err, fees, rents)
			}
			if f.Status.ConfirmationStatus != "finalized" || f.Status.Slot != f.Receipt.Slot || string(f.Meta.Err) != "null" || f.Meta.Fee != 10000 ||
				f.Meta.PostBalances[0]-f.Meta.PreBalances[0] != -(f.Meta.Fee+f.RentLamports) {
				t.Fatal("finalized fee payer debit differs")
			}
			wire, err := base64.StdEncoding.DecodeString(f.Receipt.Transaction[0])
			final, decodeErr := base64.StdEncoding.DecodeString(f.Fees[1].MessageBase64)
			const offset = 1 + 2*ed25519.SignatureSize
			if err != nil || decodeErr != nil || len(wire) != offset+len(final) || wire[0] != 2 || !bytes.Equal(wire[offset:], final) || f.Signature != base58.Encode(wire[1:1+ed25519.SignatureSize]) {
				t.Fatal("receipt differs from priced message")
			}
			for i, address := range []string{f.Addresses.Sponsor, f.Addresses.User} {
				key, err := base58.Decode(address)
				start := 1 + i*ed25519.SignatureSize
				if err != nil || !ed25519.Verify(key, final, wire[start:start+ed25519.SignatureSize]) {
					t.Fatal("invalid priced message signature", i)
				}
			}
		})
	}
}

func TestUSDCDepositRejectsUnusableAccounts(t *testing.T) {
	const mint = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	account := func(owner, state, atoms string) any {
		return map[string]any{"owner": "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "data": map[string]any{"parsed": map[string]any{
			"type": "account", "info": map[string]any{"mint": mint, "owner": owner, "state": state, "tokenAmount": map[string]any{"amount": atoms, "decimals": 6}}}}}
	}
	src, dst := account(solOwner, "initialized", "10000"), account(devnetDepositRecipient, "initialized", "0")
	for _, tc := range []struct {
		name         string
		source, dest any
		code         apperror.Code
	}{
		{"missing source", nil, dst, apperror.InsufficientBalance}, {"missing rent", src, nil, apperror.ServiceUnavailable},
		{"fee exceeds source", src, dst, apperror.InsufficientBalance}, {"frozen recipient", src, account(devnetDepositRecipient, "frozen", "0"), apperror.ServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
				method, _ := rpcInput(t, r)
				switch method {
				case "getGenesisHash":
					rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
				case "getLatestBlockhash":
					rpcResult(w, map[string]any{"value": SolanaBlock{depositBlockhash, 123}})
				case "getMultipleAccounts":
					rpcResult(w, map[string]any{"context": map[string]int{"slot": 1}, "value": []any{tc.source, tc.dest}})
				case "getMinimumBalanceForRentExemption":
					rpcResult(w, nil)
				case "getFeeForMessage":
					rpcResult(w, map[string]any{"context": map[string]int{"slot": 1}, "value": 10000})
				default:
					t.Error(method)
				}
			}, func(c *Config) {
				c.DepositQuotes = solanaQuoteSheet()
				c.DepositQuotes.SolanaUSDC = c.DepositQuotes.SolanaSOL
				c.DepositQuotes.Assets = append(c.DepositQuotes.Assets, DepositQuoteRate{SolanaDevnetUSDC, "1", "0"})
			})
			q, err := s.PreviewWalletDeposit(context.Background(), 7, SolanaDevnetUSDC, "0.01", solOwner)
			var actual *apperror.Error
			if !errors.As(err, &actual) || actual.Code != tc.code || q.PayQuantity != "" {
				t.Fatal(q, err)
			}
		})
	}
}

func TestSolanaTestUSDTDepositUsesConfiguredMint(t *testing.T) {
	const id = SolanaDevnetTestUSDTPrefix + depositTestUSDTMint
	const usdcMint = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	for _, tc := range []struct{ mint, rateID string }{
		{"", ""}, {depositTestUSDTMint, ""}, {usdcMint, SolanaDevnetTestUSDTPrefix + usdcMint}, {solOwner, id},
	} {
		cfg := Config{Networks: []Endpoint{{Network: SolanaDevnet, URL: "http://127.0.0.1:1", TestUSDTContract: tc.mint}}, DepositQuotes: solanaQuoteSheet()}
		cfg.DepositQuotes.SolanaTestUSDT = cfg.DepositQuotes.SolanaSOL
		if tc.rateID != "" {
			cfg.DepositQuotes.Assets = append(cfg.DepositQuotes.Assets, DepositQuoteRate{tc.rateID, "0.98", "0"})
		}
		if s, err := New(cfg, nil); err == nil {
			s.Close()
			t.Fatalf("accepted inconsistent USDT route: mint=%q rate=%q", tc.mint, tc.rateID)
		}
	}
	s := service(t, SolanaDevnet, func(http.ResponseWriter, *http.Request) { t.Error("unexpected RPC") }, func(c *Config) {
		c.Networks[0].TestUSDTContract = depositTestUSDTMint
		c.DepositQuotes = solanaQuoteSheet()
		c.DepositQuotes.Assets = append(c.DepositQuotes.Assets, DepositQuoteRate{id, "0.98", "0"})
	})
	q, err := s.PreviewDeposit(id, "0.01", time.Now())
	if err != nil || q.WalletFeePreview || q.USDPerCoin != "0.98" {
		t.Fatal(q, err)
	}
	for assetID, want := range map[string]apperror.Code{
		id: apperror.ServiceUnavailable, SolanaDevnet + ":usdt": apperror.InvalidArgument, SolanaDevnetTestUSDTPrefix + solOwner: apperror.InvalidArgument,
	} {
		_, err := s.PreviewWalletDeposit(context.Background(), 7, assetID, "0.01", solOwner)
		var got *apperror.Error
		if !errors.As(err, &got) || got.Code != want {
			t.Fatalf("%s: %v", assetID, err)
		}
	}
}
