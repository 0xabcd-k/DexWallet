package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/xssnick/tonutils-go/tlb"
)

const tonDepositUser = "0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a"
const tonDepositPlatform = "0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0"

type tonCapture struct {
	Absent   []string
	Head     map[string]any
	Accounts map[string]map[string]any
	Attempts []struct {
		Boc   string
		Trace tonDepositTrace
	}
	Preview DepositPreview
}

type tonDepositCase struct {
	name   string
	change func(*tonCapture)
	want   apperror.Code
}

func TestTONNativeDeposit(t *testing.T) {
	const unavailable = apperror.ServiceUnavailable
	cases := []tonDepositCase{
		{"captured funded W5", nil, ""},
		{"user cannot pay gross", func(c *tonCapture) { c.Accounts[tonDepositUser]["balance"] = 10000000 }, apperror.InsufficientBalance},
		{"platform cannot dispatch", func(c *tonCapture) { c.Accounts[tonDepositPlatform]["balance"] = TONDepositAttachmentNano }, unavailable},
		{"wrong network", func(c *tonCapture) { c.Head["global_id"] = -239 }, unavailable},
		{"stale testnet head", func(c *tonCapture) { c.Head["gen_utime"] = 1 }, unavailable},
		{"inactive source", func(c *tonCapture) { c.Accounts[tonDepositUser]["status"] = "uninit" }, apperror.InvalidArgument},
		{"unsupported wallet code", func(c *tonCapture) { c.Accounts[tonDepositUser]["code"] = c.Accounts[tonDepositUser]["data"] }, apperror.InvalidArgument},
		{"state belongs to another wallet", func(c *tonCapture) { c.Accounts[tonDepositUser]["data"] = c.Accounts[tonDepositPlatform]["data"] }, apperror.InvalidArgument},
		{"missing payment leg", func(c *tonCapture) { c.Attempts[0].Trace.Children[0].Children = nil }, unavailable},
		{"unexpected second payout", func(c *tonCapture) { tr := &c.Attempts[0].Trace; tr.Children = append(tr.Children, tr.Children[0]) }, unavailable},
		{"trace for other amount", func(c *tonCapture) { c.Attempts[0].Trace = c.Attempts[1].Trace }, unavailable},
		{"unrelated receipt", func(c *tonCapture) {
			c.Attempts[0].Trace.Children[0].Children = c.Attempts[1].Trace.Children[0].Children
		}, unavailable},
	}
	testTONDeposit(t, "ton-native-deposit-emulation", tonDepositPlatform, cases)
}
func TestTONJettonDeposit(t *testing.T) {
	const unavailable = apperror.ServiceUnavailable
	const sender = "0:90a51a0fd3524a8db2fd03b66567fa3c4c6f5b2153df8340558a440e10e897c2"
	const receiver = "0:db0cadcf093b72ab1d5cdd0db6daec3be55fab0c6a0665a99cbdc561a81248a0"
	setWalletData := func(owner string, status uint8, atoms int64) func(*tonCapture) {
		return func(c *tonCapture) {
			a := c.Accounts[owner]
			cell, err := tonDepositCell(a["data"].(string))
			if err != nil {
				t.Fatal(err)
			}
			var v tonJettonWalletData
			if err := tlb.LoadFromCell(&v, cell.BeginParse()); err != nil {
				t.Fatal(err)
			}
			v.Status = status
			v.Tokens = tlb.FromNanoTON(big.NewInt(atoms))
			result, err := tlb.ToCell(v)
			if err != nil {
				t.Fatal(err)
			}
			a["data"] = hex.EncodeToString(result.ToBOC())
		}
	}

	testTONDeposit(t, "ton-jetton-deposit-existing", tonDepositPlatform, []tonDepositCase{
		{"reuse receiver", nil, ""},
		{"exact token balance", setWalletData(sender, 0, 17745), ""},
		{"one token unit short of gross", setWalletData(sender, 0, 17744), apperror.InsufficientBalance},
		{"sender locked", setWalletData(sender, jettonSendLocked, 9000000), apperror.InvalidArgument},
		{"recipient locked", setWalletData(receiver, jettonReceiveLocked, 999000), unavailable},
		{"user W5 needs no TON", func(c *tonCapture) { c.Accounts[tonDepositUser]["balance"] = 0 }, ""},
		{"missing source tokens", func(c *tonCapture) { c.Accounts[sender]["status"] = "nonexist" }, apperror.InsufficientBalance},
		{"wrong master code", func(c *tonCapture) { c.Accounts[tonDepositMaster]["code"] = c.Accounts[sender]["code"] }, unavailable},
		{"wrong token wallet owner", func(c *tonCapture) { c.Accounts[sender]["data"] = c.Accounts[receiver]["data"] }, unavailable},
		{"platform missing dispatch cost", func(c *tonCapture) {
			c.Accounts[tonDepositPlatform]["balance"] = tonJettonActionNano + TONDepositAttachmentNano
		}, unavailable},
		{"receipt state has changed", func(c *tonCapture) { c.Accounts[receiver]["status"] = "uninit" }, unavailable},
		{"missing refund", func(c *tonCapture) {
			r := &c.Attempts[0].Trace.Children[0].Children[0].Children[0]
			r.Children = r.Children[:1]
		}, unavailable},
		{"wrong refund", func(c *tonCapture) {
			r := &c.Attempts[0].Trace.Children[0].Children[0].Children[0]
			r.Children[1] = r.Children[0]
		}, unavailable},
		{"different amount", func(c *tonCapture) { c.Attempts[0].Trace = c.Attempts[1].Trace }, unavailable},
		{"refund and notification in reverse order", func(c *tonCapture) {
			for i := range c.Attempts {
				links := c.Attempts[i].Trace.Children[0].Children[0].Children[0].Children
				links[0], links[1] = links[1], links[0]
			}
		}, ""},
	})
	const freshPlatform = "0:416e30562661c5df1a0599da1007963d7efed3c1b547d272f24c0cb77765fc93"
	const freshTokenWallet = "0:ba6991a8db92736776a05d93046372e3c390fa9ac3de821b2bb74eda1c2c6c12"
	testTONDeposit(t, "ton-jetton-deposit-first", freshPlatform, []tonDepositCase{
		{"first platform token receipt", nil, ""},
		{"missing raw state with active summary", func(c *tonCapture) { c.Accounts[freshTokenWallet]["status"] = "active" }, unavailable},
	})
}

func testTONDeposit(t *testing.T, name, platform string, cases []tonDepositCase) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f tonCapture
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			now := time.UnixMilli(f.Preview.QuotedAt)
			blockTime := int64(f.Head["gen_utime"].(float64))
			if tc.change != nil {
				tc.change(&f)
			}
			attempts := 0
			s := service(t, TONTestnet, func(w http.ResponseWriter, r *http.Request) {
				var result any
				switch {
				case r.Method == "GET" && r.URL.Path == "/v2/blockchain/masterchain-head":
					result = f.Head
				case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/blockchain/accounts/"):
					owner := strings.TrimPrefix(r.URL.Path, "/v2/blockchain/accounts/")
					if slices.Contains(f.Absent, owner) {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					result = f.Accounts[owner]
				case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/accounts/"):
					owner := strings.TrimPrefix(r.URL.Path, "/v2/accounts/")
					if !slices.Contains(f.Absent, owner) {
						t.Errorf("unexpected TON summary %s", owner)
					}
					result = f.Accounts[owner]
				case r.Method == "POST" && r.URL.Path == "/v2/traces/emulate" && r.URL.Query().Get("ignore_signature_check") == "true":
					var req struct{ Boc string }
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil || attempts >= len(f.Attempts) || req.Boc != f.Attempts[attempts].Boc {
						t.Error("unexpected emulator input", attempts, err)
						w.WriteHeader(400)
						return
					}
					result = f.Attempts[attempts].Trace
					attempts++
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					w.WriteHeader(404)
					return
				}
				json.NewEncoder(w).Encode(result)
			}, func(c *Config) {
				c.DepositQuotes = &DepositQuoteConfig{Version: f.Preview.Version, AsOf: now.Unix() - 60, ValidUntil: now.Unix() + 180, Assets: []DepositQuoteRate{{tonDepositAsset, "1", "0.001"}}, TONNative: &TONDepositFeeConfig{platform, c.Networks[0].URL}}
				if f.Preview.AssetID != tonDepositAsset {
					c.Networks[0].TestUSDTContract = tonDepositMaster
					c.DepositQuotes.TONTestUSDT = c.DepositQuotes.TONNative
					c.DepositQuotes.TONNative = nil
					c.DepositQuotes.Assets = []DepositQuoteRate{{tonDepositAsset, "2", "0.001"}, {f.Preview.AssetID, "0.98", "0.001"}}
				}
			})
			rpcTime := now
			q, err := s.previewTONDeposit(context.Background(), 1, f.Preview.AssetID, "0.01", tonDepositUser, func() time.Time {
				v := rpcTime
				head := time.Unix(blockTime, 0)
				if head.After(rpcTime) {
					rpcTime = head
				}
				return v
			})
			if tc.want != "" {
				var coded *apperror.Error
				if !errors.As(err, &coded) || coded.Code != tc.want {
					t.Fatalf("got %v, want %s", err, tc.want)
				}
				return
			}
			// The original RPC capture predates the stored order terms.
			f.Preview.FeePayer, f.Preview.Recipient = platform, platform
			f.Preview.RuleVersion = f.Preview.Version
			f.Preview.PayDecimals, f.Preview.GasDecimals, f.Preview.GasUSDPerCoin = 9, 9, "1"
			if f.Preview.AssetID != tonDepositAsset {
				f.Preview.PayDecimals, f.Preview.GasUSDPerCoin = 6, "2"
			}
			if err != nil || q != f.Preview || attempts != len(f.Attempts) {
				t.Fatal(q, err, attempts)
			}
		})
	}
}

func TestTONJettonDepositRequiresVerifiedMaster(t *testing.T) {
	route := &TONDepositFeeConfig{PlatformWallet: tonDepositPlatform, TonAPIURL: "https://testnet.tonapi.io"}
	for _, tc := range []struct {
		master    string
		supported bool
	}{
		{"kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy", true},
		{tonDepositUser, false},
	} {
		id := asset(TONTestnet, "USDT", tc.master, tonJettonDecimals, TestFixture).ID
		c := Config{Networks: []Endpoint{{Network: TONTestnet, URL: route.TonAPIURL, TestUSDTContract: tc.master}},
			DepositQuotes: &DepositQuoteConfig{TONTestUSDT: route, Version: "w5", AsOf: 1, ValidUntil: 100, Assets: []DepositQuoteRate{{tonDepositAsset, "2", "0.001"}, {id, "0.98", "0.001"}}}}
		s, err := New(c, nil)
		if err != nil {
			if tc.supported {
				t.Fatal(err)
			}
			continue
		}
		defer s.Close()
		if !tc.supported {
			t.Fatal("enabled another Jetton master")
		}
		quote, err := s.PreviewDeposit(id, "0.01", time.Unix(50, 0))
		if err != nil || !quote.WalletFeePreview {
			t.Fatal(quote, err)
		}
	}
}
