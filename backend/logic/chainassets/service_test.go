package chainassets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
)

type limitFunc func(context.Context, int64) error

func (f limitFunc) Allow(c context.Context, u int64) error { return f(c, u) }

var allow = limitFunc(func(context.Context, int64) error { return nil })

const evmOwner = "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
const solOwner = "9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf"
const tonOwner = "UQBHyu-oZVDHRYQ1-rKlGqpHy5yAqanPBirEQNMNOmfHLtaT"
const tronOwner = "TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio"

func service(t *testing.T, network string, handler http.HandlerFunc, configure ...func(*Config)) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := Config{Networks: []Endpoint{{Network: network, URL: server.URL}}}
	for _, change := range configure {
		change(&c)
	}
	s, err := New(c, allow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func rpcResult(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}
func rpcInput(t *testing.T, r *http.Request) (string, []json.RawMessage) {
	t.Helper()
	var b struct {
		Method string
		Params []json.RawMessage
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		t.Error(err)
	}
	return b.Method, b.Params
}
func query(t *testing.T, s *Service, n, owner string) Result {
	t.Helper()
	r, err := s.Query(context.Background(), 7, n, owner)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestCatalogIdentitiesAndConfiguration(t *testing.T) {
	s, err := New(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	count := 0
	for _, n := range s.Catalog() {
		if n.Enabled {
			t.Fatal("default network enabled")
		}
		if !n.Testnet {
			count += len(n.Assets)
		}
		for _, a := range n.Assets {
			if a.Contract != "" {
				if _, err := normalizeAddress(n, a.Contract); err != nil {
					t.Fatalf("%s: %v", a.ID, err)
				}
			}
		}
	}
	if count != 9 {
		t.Fatalf("mainnet assets %d", count)
	}
	for _, n := range []string{TONMainnet, BaseSepolia} {
		if err := (Config{Networks: []Endpoint{{Network: n, URL: "https://example.com", TestUSDTContract: tonOwner}}}).Check(); err == nil {
			t.Fatalf("accepted substitute %s", n)
		}
	}
	for _, url := range []string{"http://example.com", "https://secret@example.com", "https://example.com/#fragment", "file:///x"} {
		if err := (Config{Networks: []Endpoint{{Network: TONMainnet, URL: url}}}).Check(); err == nil {
			t.Fatalf("accepted endpoint %s", url)
		}
	}
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_RPC_URL", "https://testnet.toncenter.com")
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_API_KEY", "private-fixture")
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_TEST_USDT_CONTRACT", tonOwner)
	c := Config{}
	c.LoadEnvironment()
	s2, err := New(c, allow)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	b, _ := json.Marshal(s2.Catalog())
	if strings.Contains(string(b), "private-fixture") || strings.Contains(string(b), "toncenter.com") {
		t.Fatal("private endpoint leaked")
	}
	if asset(TONTestnet, "USDT", "old-master", 6, TestFixture).ID == asset(TONTestnet, "USDT", "new-master", 6, TestFixture).ID {
		t.Fatal("replacement test token reused the same identity")
	}
	for _, n := range s2.Catalog() {
		if n.ID == TONTestnet && (len(n.Assets) != 2 || n.Assets[1].Origin != TestFixture) {
			t.Fatal(n)
		}
	}
}
func TestAddressBoundaryAndRateLimitPreventUpstreamCalls(t *testing.T) {
	var calls atomic.Int32
	s := service(t, BaseMainnet, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, owner := range []string{"", evmOwner + " ", "0x9858effD232B4033E47d90003D41EC34EcaEda94", tronOwner} {
		if _, err := s.Query(context.Background(), 7, BaseMainnet, owner); err == nil {
			t.Fatal("accepted invalid owner", owner)
		}
	}
	s.limiter = limitFunc(func(_ context.Context, u int64) error {
		if u != 7 {
			t.Error("wrong user")
		}
		return apperror.New(apperror.RateLimited, nil)
	})
	_, err := s.Query(context.Background(), 7, BaseMainnet, evmOwner)
	var app *apperror.Error
	if !errors.As(err, &app) || app.Code != apperror.RateLimited {
		t.Fatal(err)
	}
	s.limiter = nil
	if _, err = s.Query(context.Background(), 7, BaseMainnet, evmOwner); err == nil {
		t.Fatal("missing limiter accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid/rate limited requests reached upstream")
	}
}
func TestBaseExactQuantitiesPartialFailureAndNetwork(t *testing.T) {
	var badToken, wrongNetwork atomic.Bool
	s := service(t, BaseMainnet, func(w http.ResponseWriter, r *http.Request) {
		m, p := rpcInput(t, r)
		switch m {
		case "eth_chainId":
			if wrongNetwork.Load() {
				rpcResult(w, "0x1")
			} else {
				rpcResult(w, "0x2105")
			}
		case "eth_getBalance":
			rpcResult(w, "0x1bc16d674ec80001")
		case "eth_call":
			var call map[string]string
			_ = json.Unmarshal(p[0], &call)
			if call["to"] != "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913" || call["data"] != "0x70a082310000000000000000000000009858effd232b4033e47d90003d41ec34ecaeda94" {
				t.Error(call)
			}
			if badToken.Load() {
				rpcResult(w, "0x")
			} else {
				rpcResult(w, fmt.Sprintf("0x%064x", 9007199254740993))
			}
		default:
			t.Error("unexpected RPC", m)
		}
	})
	r := query(t, s, BaseMainnet, evmOwner)
	if r.Items[0].Quantity != "2.000000000000000001" || r.Items[1].Quantity != "9007199254.740993" {
		t.Fatal(r)
	}
	badToken.Store(true)
	r = query(t, s, BaseMainnet, evmOwner)
	if r.Items[0].Status != StatusOK || r.Items[1].Quantity != "" || r.Items[1].ErrorCode != ErrorInvalidResponse {
		t.Fatal(r)
	}
	wrongNetwork.Store(true)
	r = query(t, s, BaseMainnet, evmOwner)
	for _, b := range r.Items {
		if b.ErrorCode != ErrorNetwork || b.Quantity != "" {
			t.Fatal(r)
		}
	}
}
func TestSolanaAggregatesAccountsAndRejectsWrongIdentity(t *testing.T) {
	var bad atomic.Bool
	s := service(t, SolanaDevnet, func(w http.ResponseWriter, r *http.Request) {
		m, p := rpcInput(t, r)
		switch m {
		case "getGenesisHash":
			rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
		case "getBalance":
			rpcResult(w, map[string]any{"value": json.Number("9007199254740993")})
		case "getTokenAccountsByOwner":
			var filter map[string]string
			_ = json.Unmarshal(p[1], &filter)
			owner := solOwner
			if bad.Load() {
				owner = "wrong-owner"
			}
			items := []any{}
			for i, state := range []string{"initialized", "frozen"} {
				items = append(items, map[string]any{"pubkey": fmt.Sprint("account", i), "account": map[string]any{"owner": splTokenProgram, "data": map[string]any{"program": "spl-token", "parsed": map[string]any{"type": "account", "info": map[string]any{"owner": owner, "mint": filter["mint"], "state": state, "tokenAmount": map[string]any{"amount": "1000001", "decimals": 6}}}}}})
			}
			rpcResult(w, map[string]any{"value": items})
		default:
			t.Error(m)
		}
	})
	r := query(t, s, SolanaDevnet, solOwner)
	if r.Items[0].Quantity != "9007199.254740993" || r.Items[1].Quantity != "2.000002" {
		t.Fatal(r)
	}
	bad.Store(true)
	r = query(t, s, SolanaDevnet, solOwner)
	if r.Items[1].ErrorCode != ErrorInvalidResponse {
		t.Fatal(r)
	}
}
func TestTONJettonMasterAndZeroSemantics(t *testing.T) {
	var mode atomic.Int32
	s := service(t, TONMainnet, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/getAddressBalance":
			fmt.Fprint(w, `{"ok":true,"result":"1234567890"}`)
		case "/api/v3/jetton/wallets":
			switch mode.Load() {
			case 0:
				fmt.Fprint(w, `{"jetton_wallets":[]}`)
			case 1:
				fmt.Fprint(w, `{}`)
			case 2:
				_ = json.NewEncoder(w).Encode(map[string]any{"jetton_wallets": []any{map[string]any{"address": tonOwner, "owner": tonOwner, "jetton": r.URL.Query().Get("jetton_address"), "balance": "1234567"}}})
			case 3:
				_ = json.NewEncoder(w).Encode(map[string]any{"jetton_wallets": []any{map[string]any{"address": tonOwner, "owner": tonOwner, "jetton": tonOwner, "balance": "1234567"}}})
			}
		default:
			t.Error(r.URL.Path)
		}
	})
	for i, want := range []string{"0", "", "1.234567", ""} {
		mode.Store(int32(i))
		r := query(t, s, TONMainnet, tonOwner)
		if r.Items[0].Quantity != "1.23456789" || r.Items[1].Quantity != want {
			t.Fatal(r)
		}
		if want == "" && r.Items[1].Status != StatusUnavailable {
			t.Fatal(r)
		}
	}
}
func TestTRONUnactivatedZeroAndTokenConstantCall(t *testing.T) {
	var bad atomic.Bool
	s := service(t, TRONMainnet, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wallet/getaccount":
			if bad.Load() {
				fmt.Fprint(w, `{"Error":"node unavailable"}`)
			} else {
				fmt.Fprint(w, `{}`)
			}
		case "/wallet/triggerconstantcontract":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["contract_address"] != "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" || b["function_selector"] != "balanceOf(address)" || b["visible"] != true {
				t.Error(b)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]bool{"result": true}, "constant_result": []string{fmt.Sprintf("%064x", 1234567)}})
		default:
			t.Error(r.URL.Path)
		}
	})
	r := query(t, s, TRONMainnet, tronOwner)
	if r.Items[0].Quantity != "0" || r.Items[1].Quantity != "1.234567" {
		t.Fatal(r)
	}
	bad.Store(true)
	r = query(t, s, TRONMainnet, tronOwner)
	if r.Items[0].Quantity != "" || r.Items[0].ErrorCode != ErrorInvalidResponse {
		t.Fatal(r)
	}
}
func TestTimeoutAndMalformedResponsesAreNotZero(t *testing.T) {
	for _, mode := range []string{"timeout", "http-error", "null", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			s := service(t, TONTestnet, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "timeout":
					<-r.Context().Done()
				case "http-error":
					w.WriteHeader(429)
				case "null":
					fmt.Fprint(w, `{"ok":true,"result":null}`)
				case "malformed":
					fmt.Fprint(w, `{"ok":true,"result":"0"}junk`)
				case "oversized":
					fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1))
				}
			}, func(c *Config) { c.Timeout = 100 * time.Millisecond })
			r := query(t, s, TONTestnet, tonOwner)
			if r.Items[0].Status != StatusUnavailable || r.Items[0].Quantity != "" {
				t.Fatal(r)
			}
		})
	}
}
