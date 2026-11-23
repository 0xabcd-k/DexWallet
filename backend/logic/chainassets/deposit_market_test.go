package chainassets

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/marketdata"
	"github.com/shopspring/decimal"
)

type readRates func(context.Context) (marketdata.Table, error)

func (f readRates) Current(ctx context.Context) (marketdata.Table, error) { return f(ctx) }

func TestMarketDepositFreshnessAndRecovery(t *testing.T) {
	now := time.Unix(10000, 0)
	table := marketdata.Table{
		"ETH":  {Symbol: "ETH", TradeID: "eth-1", USDPerCoin: decimal.NewFromInt(2000), ObservedMicros: now.Add(-55 * time.Second).UnixMicro()},
		"USDC": {Symbol: "USDC", TradeID: "usdc-1", USDPerCoin: decimal.New(98, -2), ObservedMicros: now.UnixMicro()},
	}
	fail := false
	reader := readRates(func(context.Context) (marketdata.Table, error) {
		if fail {
			return nil, errors.New("redis unavailable")
		}
		return table, nil
	})
	usdc := BaseSepolia + ":usdc"
	c := Config{DepositQuotes: &DepositQuoteConfig{Version: "fees-1", Market: &DepositMarketConfig{marketdata.Kraken, time.Minute}, Assets: []DepositQuoteRate{
		{BaseSepoliaETH, "", "0"}, {usdc, "", "0.0001"}, {solanaDepositAsset, "", "0"},
	}}}
	s, err := NewWithMarketRates(c, nil, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := s.PreviewDeposit(usdc, "10", now)
	if err != nil || q.RateSource != marketdata.Kraken || !strings.HasPrefix(q.Version, "kraken-") || q.USDPerCoin != "0.98" || q.PayQuantity != "10.408164" || q.GasUSD != "0.2" || q.NetUSD != "10.00" || q.AsOf != now.Add(-55*time.Second).UnixMilli() || q.ExpiresAt != now.Add(5*time.Second).UnixMilli() {
		t.Fatal(q, err)
	}
	active, err := s.DepositAvailability(context.Background(), now)
	if err != nil || !active[usdc] || active[solanaDepositAsset] {
		t.Fatal(active, err)
	}
	later := now.Add(5 * time.Second)
	if _, err = s.PreviewDeposit(usdc, "10", later); err == nil {
		t.Fatal("used expired gas price")
	}
	r := table["ETH"]
	r.ObservedMicros = later.UnixMicro()
	r.TradeID = "eth-2"
	table["ETH"] = r
	next, err := s.PreviewDeposit(usdc, "10", later)
	if err != nil || next.Version == q.Version || next.ExpiresAt != later.Add(depositPreviewTTL).UnixMilli() {
		t.Fatal(next, err)
	}
	fail = true
	if _, err = s.PreviewDeposit(usdc, "10", later); err == nil {
		t.Fatal("used static rate during a store failure")
	}
	c.DepositQuotes.Assets[0].USDPerCoin = "1"
	if mixed, err := NewWithMarketRates(c, nil, reader); err == nil {
		mixed.Close()
		t.Fatal("mixed static and exchange prices")
	}
}

func TestSolanaWalletUsesOneMarketSnapshot(t *testing.T) {
	serverCalls, reads := 0, 0
	server := httptestSolanaFee(t, &serverCalls)
	now := time.Now()
	cfg := solanaQuoteSheet()
	cfg.AsOf, cfg.ValidUntil, cfg.Assets[0].USDPerCoin = 0, 0, ""
	cfg.Market = &DepositMarketConfig{marketdata.Kraken, time.Minute}
	reader := readRates(func(context.Context) (marketdata.Table, error) {
		reads++
		return marketdata.Table{"SOL": {Symbol: "SOL", USDPerCoin: decimal.NewFromInt(100 * int64(reads)), TradeID: "trade", ObservedMicros: now.UnixMicro()}}, nil
	})
	s, err := NewWithMarketRates(Config{DepositQuotes: cfg, Networks: []Endpoint{{Network: SolanaDevnet, URL: server.URL}}}, allow, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := s.PreviewWalletDeposit(context.Background(), 7, solanaDepositAsset, "0.01", solOwner)
	if err != nil || reads != 1 || serverCalls != 2 || q.GasUSD != "0.001" || q.PayQuantity != "0.00011" {
		t.Fatal(q, err, reads, serverCalls)
	}
}

func httptestSolanaFee(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, _ := rpcInput(t, r)
		switch method {
		case "getGenesisHash":
			rpcResult(w, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG")
		case "getLatestBlockhash":
			rpcResult(w, map[string]any{"value": SolanaBlock{depositBlockhash, 123}})
		case "getFeeForMessage":
			(*calls)++
			rpcResult(w, map[string]any{"context": map[string]int{"slot": 1}, "value": 10000})
		default:
			t.Error(method)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
