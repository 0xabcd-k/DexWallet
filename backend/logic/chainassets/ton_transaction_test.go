package chainassets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTONEvidenceNetworkAndExactQueries(t *testing.T) {
	data, err := os.ReadFile("../chainreceipt/testdata/ton-jetton.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request  struct{ Hash, Recipient string }
		Evidence TONEvidence
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	var target struct {
		Transactions []struct {
			Block struct {
				Workchain int    `json:"workchain"`
				Shard     string `json:"shard"`
				Seqno     int64  `json:"seqno"`
			} `json:"block_ref"`
		}
	}
	_ = json.Unmarshal(f.Evidence.Transaction, &target)
	master := "kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy"
	network := Network{Family: TON, Testnet: true}
	canonical, _ := normalizeAddress(network, master)
	owner, _ := normalizeAddress(network, f.Request.Recipient)
	var calls, mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-API-Key") != "test-only" {
			t.Error("missing configured API key")
		}
		result := json.RawMessage(`{}`)
		switch r.URL.Path {
		case "/api/v2/getMasterchainInfo":
			result = f.Evidence.Masterchain
			if mode.Load() == 1 {
				result = json.RawMessage(`{"ok":true,"result":{"init":{"root_hash":"` + TONMainnetZeroState + `"}}}`)
			}
		case "/api/v3/transactions":
			if r.URL.Query().Get("hash") != f.Request.Hash || r.URL.Query().Get("limit") != "2" {
				t.Error(r.URL.Query())
			}
			result = f.Evidence.Transaction
			switch mode.Load() {
			case 2:
				result = json.RawMessage(`{"transactions":[]}`)
			case 3:
				result = json.RawMessage(`null`)
			case 4:
				result = json.RawMessage(`{"error":"index unavailable"}`)
			}
		case "/api/v3/adjacentTransactions":
			if r.URL.Query().Get("hash") != f.Request.Hash || r.URL.Query().Get("direction") != "in" {
				t.Error(r.URL.Query())
			}
			result = f.Evidence.Parent
		case "/api/v3/blocks":
			if r.URL.Query().Get("workchain") != "0" || r.URL.Query().Get("shard") != target.Transactions[0].Block.Shard || r.URL.Query().Get("seqno") == "" {
				t.Error(r.URL.Query())
			}
			result = f.Evidence.Block
		case "/api/v3/runGetMethod":
			var req struct {
				Address, Method string
				Stack           []struct{ Type, Value string }
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&req) != nil || req.Address != canonical || req.Method != "get_wallet_address" || len(req.Stack) != 1 || req.Stack[0].Type != "slice" {
				t.Error("wrong get method request")
				return
			}
			b, err := base64.StdEncoding.DecodeString(req.Stack[0].Value)
			if err != nil {
				t.Error(err)
				return
			}
			c, err := cell.FromBOC(b)
			if err != nil {
				t.Error(err)
				return
			}
			a, err := c.BeginParse().LoadAddr()
			if err != nil || a.StringRaw() != owner {
				t.Error("wrong owner stack")
			}
			result = f.Evidence.Wallet
		default:
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(result)
	}))
	defer server.Close()
	source, err := New(Config{Networks: []Endpoint{{Network: TONTestnet, URL: server.URL, APIKey: "test-only", TestUSDTContract: master}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	proof, err := source.ReadTONEvidence(context.Background(), TONTestnet, f.Request.Hash, master, f.Request.Recipient)
	if err != nil || calls.Load() != 5 || string(proof.Wallet) != string(f.Evidence.Wallet) {
		t.Fatal(err, calls.Load())
	}
	for _, tt := range []struct {
		mode, calls int32
		wantError   bool
	}{{1, 1, true}, {2, 2, false}, {3, 2, true}, {4, 2, true}} {
		mode.Store(tt.mode)
		calls.Store(0)
		_, err = source.ReadTONEvidence(context.Background(), TONTestnet, f.Request.Hash, "", "")
		if (err != nil) != tt.wantError || calls.Load() != tt.calls {
			t.Fatal(tt, err, calls.Load())
		}
	}
	calls.Store(0)
	for _, n := range []string{TONMainnet, TRONNile} {
		if _, err = source.ReadTONEvidence(context.Background(), n, f.Request.Hash, "", ""); err == nil {
			t.Fatal(n)
		}
	}
	if _, err = source.ReadTONEvidence(context.Background(), TONTestnet, f.Request.Hash, owner, f.Request.Recipient); err == nil || calls.Load() != 0 {
		t.Fatal("unlisted master accepted", err)
	}
}
func TestTONPublicRequestSpacingHonorsCancellation(t *testing.T) {
	calls := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"init":{"root_hash":"` + TONTestnetZeroState + `"}}}`))
	}))
	defer server.Close()
	source, err := New(Config{Timeout: 50 * time.Millisecond, Networks: []Endpoint{{Network: TONTestnet, URL: server.URL}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, err = source.ReadTONEvidence(context.Background(), TONTestnet, "53d778fb27fe4ed339ba2514b5f3938964dcbc48b6b88a66fdea326cf5e25e98", "", "")
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}
