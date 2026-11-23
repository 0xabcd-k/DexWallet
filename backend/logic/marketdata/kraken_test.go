package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestKrakenUSDTrade(t *testing.T) {
	const observed = "2025-10-01T03:02:01.123456789Z"
	when, err := time.Parse(time.RFC3339Nano, observed)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, quote, price, stamp string
		ok                        bool
	}{
		{"USD trade", "USD", "0.98456789", observed, true},
		{"stablecoin quote", "USDT", "0.98", observed, false},
		{"zero price", "USD", "0", observed, false},
		{"unbounded number", "USD", "1e50", observed, false},
		{"missing timestamp", "USD", "0.98", "", false},
		{"future timestamp", "USD", "0.98", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("symbol") != "USDC/USD" || r.URL.Query().Get("count") != "1" {
					t.Error(r.URL)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"error": []string{}, "result": map[string]any{"trades": []any{map[string]string{
					"symbol": "USDC/USD", "base_asset": "USDC", "quote_asset": tc.quote, "trade_id": "verified-trade", "price": tc.price, "trade_ts": tc.stamp,
				}}}})
			}))
			defer server.Close()
			r, err := fetch(context.Background(), server.Client(), server.URL, "USDC")
			if (err == nil) != tc.ok {
				t.Fatal(r, err)
			}
			if tc.ok && (r.USDPerCoin.String() != tc.price || r.TradeID != "verified-trade" || r.ObservedMicros != when.UnixMicro()) {
				t.Fatal(r)
			}
		})
	}
}
