package chainassets

import (
	"fmt"
	"regexp"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

// A versioned operator-supplied test sheet. Times are Unix seconds and
// EstimatedGas is expressed in the network's native coin, per payment asset.
type DepositQuoteConfig struct {
	Version    string
	AsOf       int64
	ValidUntil int64
	Assets     []DepositQuoteRate
}

type DepositQuoteRate struct {
	AssetID      string
	USDPerCoin   string
	EstimatedGas string
}

const depositPreviewTTL = 30 * time.Second

var rateSyntax = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)
var versionSyntax = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type depositAsset struct {
	network       Network
	asset, native Asset
	rate, gas     decimal.Decimal
}

type depositBook struct {
	version     string
	asOf, until int64
	assets      map[string]depositAsset
}

func newDepositBook(c *DepositQuoteConfig, networks []Network) (depositBook, error) {
	var book depositBook
	if c == nil {
		return book, nil
	}
	if !versionSyntax.MatchString(c.Version) || c.AsOf <= 0 || c.ValidUntil <= c.AsOf || len(c.Assets) == 0 {
		return book, fmt.Errorf("invalid deposit quote sheet version, time window or empty asset list")
	}
	book = depositBook{version: c.Version, asOf: c.AsOf, until: c.ValidUntil, assets: make(map[string]depositAsset)}
	catalog := make(map[string]depositAsset)
	for _, n := range networks {
		if n.Testnet {
			for _, a := range n.Assets {
				catalog[a.ID] = depositAsset{network: n, asset: a, native: n.Assets[0]}
			}
		}
	}
	for _, row := range c.Assets {
		entry, ok := catalog[row.AssetID]
		if _, repeat := book.assets[row.AssetID]; !ok || repeat {
			return book, fmt.Errorf("unknown, non-testnet or repeated quote asset %q", row.AssetID)
		}
		if !rateSyntax.MatchString(row.USDPerCoin) {
			return book, fmt.Errorf("invalid USD rate for %s", row.AssetID)
		}
		entry.rate, _ = decimal.NewFromString(row.USDPerCoin)
		if !entry.rate.IsPositive() {
			return book, fmt.Errorf("zero USD rate for %s", row.AssetID)
		}
		// Only an explicitly configured zero represents zero estimated cost.
		if row.EstimatedGas != "0" {
			if _, err := chainQuantity(entry.network, entry.native, row.EstimatedGas); err != nil {
				return book, fmt.Errorf("invalid native gas estimate for %s: %w", row.AssetID, err)
			}
		}
		entry.gas, _ = decimal.NewFromString(row.EstimatedGas)
		book.assets[row.AssetID] = entry
	}
	for _, entry := range book.assets {
		if _, ok := book.assets[entry.native.ID]; !ok {
			return book, fmt.Errorf("missing native USD rate %s for %s", entry.native.ID, entry.asset.ID)
		}
	}
	return book, nil
}

func (s *Service) HasDepositPreview(assetID string, now time.Time) bool {
	_, ok := s.quotes.assets[assetID]
	return ok && s.quotes.asOf <= now.Unix() && now.Unix() < s.quotes.until
}

type DepositPreview struct {
	AssetID, TargetUSD, USDPerCoin, PayQuantity string
	GasAssetID, EstimatedGas, GasUSD, NetUSD    string
	Version                                     string
	AsOf, QuotedAt, ExpiresAt                   int64
}

func (s *Service) PreviewDeposit(assetID, targetUSD string, now time.Time) (DepositPreview, error) {
	var empty DepositPreview
	if !s.HasDepositPreview(assetID, now) {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("no current test quote for %q", assetID))
	}
	usd, _ := account.Lookup(account.KindUSD)
	if !rateSyntax.MatchString(targetUSD) {
		return empty, badTarget("invalid target USD amount")
	}
	target, _ := decimal.NewFromString(targetUSD)
	if !target.IsPositive() || target.Exponent() < -usd.Scale {
		return empty, badTarget("target USD must be a positive amount in cents")
	}
	entry := s.quotes.assets[assetID]
	gasUSD := entry.gas.Mul(s.quotes.assets[entry.native.ID].rate)
	scale := int32(entry.asset.Decimals)
	quantity, remainder := target.Add(gasUSD).QuoRem(entry.rate, scale)
	if !remainder.IsZero() {
		quantity = quantity.Add(decimal.New(1, -scale))
	}
	pay, err := chainQuantity(entry.network, entry.asset, quantity.String())
	if err != nil {
		return empty, err
	}
	net := quantity.Mul(entry.rate).Sub(gasUSD).Truncate(usd.Scale)
	if err := usd.ValidateBalances(net, decimal.Zero); err != nil {
		return empty, badTarget("net USD exceeds account capacity")
	}
	expires := now.Add(depositPreviewTTL)
	if expires.Unix() >= s.quotes.until {
		expires = time.Unix(s.quotes.until, 0)
	}
	return DepositPreview{
		AssetID: assetID, TargetUSD: target.StringFixed(usd.Scale), USDPerCoin: entry.rate.String(), PayQuantity: pay,
		GasAssetID: entry.native.ID, EstimatedGas: entry.gas.String(), GasUSD: gasUSD.String(), NetUSD: net.StringFixed(usd.Scale),
		Version: s.quotes.version, AsOf: time.Unix(s.quotes.asOf, 0).UnixMilli(), QuotedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(),
	}, nil
}
