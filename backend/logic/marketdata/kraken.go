// Package marketdata maintains the platform's current USD exchange rates.
package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const Kraken = "kraken"
const postTradeURL = "https://api.kraken.com/0/public/PostTrade"

var symbols = [...]string{"ETH", "SOL", "TON", "TRX", "USDC", "USDT"}
var priceSyntax = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)

type Rate struct {
	Symbol         string
	USDPerCoin     decimal.Decimal
	TradeID        string
	ObservedMicros int64
}

type Table map[string]Rate

func (r Rate) ObservedAt() time.Time { return time.UnixMicro(r.ObservedMicros) }

func fetch(ctx context.Context, client *http.Client, endpoint, symbol string) (Rate, error) {
	pair := symbol + "/USD"
	query := url.Values{"symbol": {pair}, "count": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return Rate{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Rate{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Rate{}, fmt.Errorf("HTTP %s", resp.Status)
	}
	var data struct {
		Error  []string
		Result struct {
			Trades []struct {
				ID            string `json:"trade_id"`
				Symbol, Price string
				Base          string    `json:"base_asset"`
				Quote         string    `json:"quote_asset"`
				Time          time.Time `json:"trade_ts"`
			}
		}
	}
	const maxResponse = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return Rate{}, err
	}
	if len(body) > maxResponse {
		return Rate{}, fmt.Errorf("oversized trade response")
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Rate{}, err
	}
	if len(data.Error) > 0 {
		return Rate{}, fmt.Errorf("exchange: %s", strings.Join(data.Error, ", "))
	}
	if len(data.Result.Trades) != 1 {
		return Rate{}, fmt.Errorf("expected one trade for %s", pair)
	}
	t := data.Result.Trades[0]
	if t.Symbol != pair || t.Base != symbol || t.Quote != "USD" || t.ID == "" || !priceSyntax.MatchString(t.Price) || t.Time.UnixMicro() <= 0 || t.Time.After(time.Now()) {
		return Rate{}, fmt.Errorf("invalid trade identity, price or timestamp for %s", pair)
	}
	price, _ := decimal.NewFromString(t.Price)
	if !price.IsPositive() {
		return Rate{}, fmt.Errorf("zero trade price for %s", pair)
	}
	return Rate{symbol, price, t.ID, t.Time.UnixMicro()}, nil
}

// Refresh updates pairs independently. An upstream failure leaves the last
// successful observation's exchange timestamp (and therefore expiry) intact.
func (s *Store) Refresh(ctx context.Context, client *http.Client) error {
	var failures []error
	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		r, err := fetch(ctx, client, postTradeURL, symbol)
		if err == nil {
			err = s.publish(ctx, r)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s %s/USD: %w", Kraken, symbol, err))
		}
	}
	return errors.Join(failures...)
}
