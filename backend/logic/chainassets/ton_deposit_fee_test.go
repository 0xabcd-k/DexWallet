package chainassets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

const tonDepositUser = "0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a"
const tonDepositPlatform = "0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0"

type tonCapture struct {
	Head     map[string]any
	Accounts map[string]map[string]any
	Attempts []struct {
		Boc   string
		Trace tonDepositTrace
	}
	Preview DepositPreview
}

func TestTONNativeDeposit(t *testing.T) {
	data, err := os.ReadFile("testdata/ton-native-deposit-emulation.json")
	if err != nil {
		t.Fatal(err)
	}
	const unavailable = apperror.ServiceUnavailable
	cases := []struct {
		name   string
		change func(*tonCapture)
		want   apperror.Code
	}{
		{"captured funded W5", nil, ""},
		{"user cannot pay gross", func(c *tonCapture) { c.Accounts[tonDepositUser]["balance"] = 10000000 }, apperror.InsufficientBalance},
		{"platform cannot dispatch", func(c *tonCapture) { c.Accounts[tonDepositPlatform]["balance"] = tonRelayNano }, unavailable},
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f tonCapture
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			now := time.UnixMilli(f.Preview.QuotedAt)
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
					result = f.Accounts[strings.TrimPrefix(r.URL.Path, "/v2/blockchain/accounts/")]
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
				c.DepositQuotes = &DepositQuoteConfig{Version: f.Preview.Version, AsOf: now.Unix() - 60, ValidUntil: now.Unix() + 180, Assets: []DepositQuoteRate{{tonDepositAsset, "1", "0.001"}}, TONNative: &TONDepositFeeConfig{tonDepositPlatform, c.Networks[0].URL}}
			})
			q, err := s.previewTONDeposit(context.Background(), 1, "0.01", tonDepositUser, func() time.Time { return now })
			if tc.want != "" {
				var coded *apperror.Error
				if !errors.As(err, &coded) || coded.Code != tc.want {
					t.Fatalf("got %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil || q != f.Preview || attempts != len(f.Attempts) {
				t.Fatal(q, err, attempts)
			}
		})
	}
}
