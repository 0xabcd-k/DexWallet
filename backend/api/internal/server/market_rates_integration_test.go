//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/marketdata"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
)

type exchangeTransport func(*http.Request) (*http.Response, error)

func (f exchangeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func checkMarketRatesHTTP(t *testing.T, s *shared.ServiceContext, token, rpcURL string, call func(string, string, string, any, int, string) (http.Header, json.RawMessage)) {
	t.Helper()
	ctx := context.Background()
	store := marketdata.NewStore(s.Redis, s.Namespace)
	if err := testRun.Record("redis", store.Key()); err != nil {
		t.Fatal(err)
	}
	id := chainassets.BaseSepolia + ":usdc"
	q := chainassets.DepositQuoteConfig{Version: "market-http", Market: &chainassets.DepositMarketConfig{Source: marketdata.Kraken, MaxAge: time.Minute}, Assets: []chainassets.DepositQuoteRate{
		{AssetID: chainassets.BaseSepoliaETH, EstimatedGas: "0"}, {AssetID: id, EstimatedGas: "0.0001"},
	}}
	q.BaseETH = &chainassets.BaseDepositFeeConfig{FeePayer: baseFeePayer, Recipient: baseFeePayer}
	networks := []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: rpcURL}}
	const nileOwner = "TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH"
	q.TRONTRX = &chainassets.TRONDepositFeeConfig{ResourceOwner: nileOwner, Recipient: nileOwner}
	q.Assets = append(q.Assets, chainassets.DepositQuoteRate{AssetID: chainassets.TRONNileTRX, EstimatedGas: "0.1"})
	networks = append(networks, chainassets.Endpoint{Network: chainassets.TRONNile, URL: nileDepositTestEndpoint(t)})
	service, err := chainassets.NewWithMarketRates(chainassets.Config{DepositQuotes: &q, Networks: networks}, chainassets.NewRedisLimiter(s.Redis, s.Namespace+":market", 1), store)
	if err != nil {
		t.Fatal(err)
	}
	old := s.ChainAssets
	s.ChainAssets = service
	defer func() { s.ChainAssets = old; service.Close() }()
	const path = "/v1/user/deposit-quotes/preview"
	in := types.DepositQuoteRequest{AssetId: id, TargetUsd: "10"}
	call("POST", path, token, in, 503, "SERVICE_UNAVAILABLE")
	observed := time.Now().UTC().Add(-2 * time.Minute)
	failUSDC := false
	client := &http.Client{Transport: exchangeTransport(func(r *http.Request) (*http.Response, error) {
		pair := r.URL.Query().Get("symbol")
		symbol := strings.TrimSuffix(pair, "/USD")
		if symbol == "USDC" && failUSDC {
			return nil, fmt.Errorf("exchange temporarily unavailable")
		}
		price := "1"
		switch symbol {
		case "TRX":
			price = "10"
		case "ETH":
			price = "2000"
		case "USDC":
			price = "0.98"
		}
		raw, _ := json.Marshal(map[string]any{"error": []string{}, "result": map[string]any{"trades": []any{map[string]string{
			"symbol": pair, "base_asset": symbol, "quote_asset": "USD", "trade_id": observed.Format(time.RFC3339Nano) + symbol, "trade_ts": observed.Format(time.RFC3339Nano), "price": price,
		}}}})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
	})}
	if err = store.Refresh(ctx, client); err != nil {
		t.Fatal(err)
	}
	call("POST", path, token, in, 503, "SERVICE_UNAVAILABLE") // Old trades just fetched are still old.
	observed = time.Now().UTC().Add(-2 * time.Second)
	if err = store.Refresh(ctx, client); err != nil {
		t.Fatal(err)
	}
	read := func() types.DepositQuoteData {
		t.Helper()
		_, raw := call("POST", path, token, in, 200, "OK")
		var got types.DepositQuoteData
		if err := json.Unmarshal(raw, &got); err != nil || got.RateSource != marketdata.Kraken || got.UsdPerCoin != "0.98" || got.PayQuantity != "10.408164" || got.NetUsd != "10.00" || got.CanSubmit {
			t.Fatal(got, err)
		}
		return got
	}
	first := read()
	eth := types.DepositQuoteRequest{AssetId: chainassets.BaseSepoliaETH, TargetUsd: "0.01", WalletAddress: baseFeeWallet}
	_, walletRaw := call("POST", path, token, eth, 200, "OK")
	var wallet types.DepositQuoteData
	if err := json.Unmarshal(walletRaw, &wallet); err != nil || wallet.FeeBlockHash != baseFeeHash || wallet.WalletAddress != eth.WalletAddress || wallet.MessageSha256 != "" || !wallet.WalletFeePreview || wallet.CanSubmit || wallet.NetUsd != "0.01" || wallet.PayQuantity != "0.000007350040469134" || wallet.RateSource != marketdata.Kraken {
		t.Fatal(wallet, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(walletRaw, &fields); err != nil || fields["delegatedBandwidth"] != nil {
		t.Fatal("unexpected bandwidth on Base quote", err)
	}
	trx := types.DepositQuoteRequest{AssetId: chainassets.TRONNileTRX, TargetUsd: "0.01", WalletAddress: "TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio"}
	_, trxRaw := call("POST", path, token, trx, 200, "OK")
	var nile types.DepositQuoteData
	if err := json.Unmarshal(trxRaw, &nile); err != nil || nile.DelegatedBandwidth != 266 || nile.EstimatedGas != "0" || nile.GasUsd != "0" || nile.PayQuantity != "0.001" || nile.NetUsd != "0.01" || nile.WalletAddress != trx.WalletAddress || len(nile.MessageSha256) != 64 || nile.RateSource != marketdata.Kraken || nile.CanSubmit {
		t.Fatal(nile, err)
	}
	_, raw := call("GET", "/v1/user/chain-assets", token, nil, 200, "OK")
	var catalog types.ChainAssetCatalogData
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, n := range catalog.Networks {
		for _, a := range n.Assets {
			if a.Id == id {
				listed = a.DepositPreview
			}
		}
	}
	if !listed {
		t.Fatal("live-priced asset missing from catalog")
	}
	observed = observed.Add(-time.Minute)
	failUSDC = true
	if err = store.Refresh(ctx, client); err == nil {
		t.Fatal("missing upstream error")
	}
	after := read()
	if after.RateVersion != first.RateVersion || after.AsOf != first.AsOf {
		t.Fatal("failed or delayed updates changed the current exchange observation", first, after)
	}
}

// Replay only the observed read-only node calls and update the head time for today's request.
func nileDepositTestEndpoint(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../logic/chainassets/testdata/tron-trx-deposit-bandwidth.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Calls []struct {
			Path           string
			Params, Result map[string]any
		}
	}
	if err = json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		for _, c := range capture.Calls {
			if c.Path == r.URL.Path && reflect.DeepEqual(c.Params, body) {
				if c.Path == "/wallet/getnowblock" {
					c.Result["block_header"].(map[string]any)["raw_data"].(map[string]any)["timestamp"] = time.Now().UnixMilli()
				}
				_ = json.NewEncoder(w).Encode(c.Result)
				return
			}
		}
		t.Errorf("unexpected Nile request %s %v", r.URL.Path, body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}
