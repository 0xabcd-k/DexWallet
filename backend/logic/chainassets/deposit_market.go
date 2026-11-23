package chainassets

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/marketdata"
	"github.com/shopspring/decimal"
)

type DepositMarketConfig struct {
	Source string
	MaxAge time.Duration
}

type MarketRateReader interface {
	Current(context.Context) (marketdata.Table, error)
}

type depositPrices struct {
	pay, native     decimal.Decimal
	asOf, until     time.Time
	version, source string
}

func (s *Service) currentRates(ctx context.Context) (marketdata.Table, error) {
	if s.quotes.market == nil {
		return nil, nil
	}
	rates, err := s.marketRates.Current(ctx)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("read USD exchange rates: %w", err))
	}
	return rates, nil
}

func (b *depositBook) prices(id string, table marketdata.Table, now time.Time) (depositPrices, bool) {
	entry, ok := b.assets[id]
	if !ok {
		return depositPrices{}, false
	}
	if b.market == nil {
		p := depositPrices{pay: entry.rate, native: b.assets[entry.native.ID].rate, asOf: time.Unix(b.asOf, 0), until: time.Unix(b.until, 0), version: b.version, source: testSheetSource}
		return p, b.asOf <= now.Unix() && now.Unix() < b.until
	}
	pay, payOK := table[entry.asset.Symbol]
	gas, gasOK := table[entry.native.Symbol]
	if !payOK || !gasOK || pay.ObservedAt().After(now) || gas.ObservedAt().After(now) {
		return depositPrices{}, false
	}
	// The older of the two trades limits how long this conversion is usable.
	asOf := time.UnixMicro(min(pay.ObservedMicros, gas.ObservedMicros))
	until := asOf.Add(b.market.MaxAge)
	if now.UnixMilli() >= until.UnixMilli() {
		return depositPrices{}, false
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%q/%q/%s/%d", b.version, id, entry.gas, b.market.MaxAge)
	for _, rate := range []marketdata.Rate{pay, gas} {
		fmt.Fprintf(hash, "/%q/%s/%d", rate.TradeID, rate.USDPerCoin, rate.ObservedMicros)
	}
	version := fmt.Sprintf("%s-%x", b.market.Source, hash.Sum(nil)[:16])
	return depositPrices{pay: pay.USDPerCoin, native: gas.USDPerCoin, asOf: asOf, until: until, version: version, source: b.market.Source}, true
}

// HasDepositPreview is the single-asset availability check. The HTTP catalog
// uses DepositAvailability to read all assets from a single observation table.
func (s *Service) HasDepositPreview(assetID string, now time.Time) bool {
	_, err := s.depositPrices(context.Background(), assetID, now)
	return err == nil
}

func (s *Service) DepositAvailability(ctx context.Context, now time.Time) (map[string]bool, error) {
	rates, err := s.currentRates(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(s.quotes.assets))
	for id := range s.quotes.assets {
		_, active[id] = s.quotes.prices(id, rates, now)
	}
	return active, nil
}

func (s *Service) depositPrices(ctx context.Context, id string, now time.Time) (depositPrices, error) {
	rates, err := s.currentRates(ctx)
	if err != nil {
		return depositPrices{}, err
	}
	prices, ok := s.quotes.prices(id, rates, now)
	if !ok {
		return depositPrices{}, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("no current USD rate for %q", id))
	}
	return prices, nil
}
