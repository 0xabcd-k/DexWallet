package chainassets

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/marketdata"
	"github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

// Gas estimates are in the network's native coin. Rate inputs come either
// from the static test sheet (Unix seconds) or from the shared market table.
type DepositQuoteConfig struct {
	SolanaUSDCReviewAfter time.Duration         `json:",optional"`
	SolanaSOLReviewAfter  time.Duration         `json:",optional"`
	TONTestUSDT           *TONDepositFeeConfig  `json:",optional"`
	TONNative             *TONDepositFeeConfig  `json:",optional"`
	TRONTestUSDT          *TRONDepositFeeConfig `json:",optional"`
	TRONTRX               *TRONDepositFeeConfig `json:",optional"`
	BaseETH               *BaseDepositFeeConfig `json:",optional"`
	BaseUSDC              *BaseDepositFeeConfig `json:",optional"`
	Version               string
	AsOf                  int64                   `json:",optional"`
	ValidUntil            int64                   `json:",optional"`
	Market                *DepositMarketConfig    `json:",optional"`
	SolanaSOL             *SolanaDepositFeeConfig `json:",optional"`
	SolanaUSDC            *SolanaDepositFeeConfig `json:",optional"`
	SolanaTestUSDT        *SolanaDepositFeeConfig `json:",optional"`
	Assets                []DepositQuoteRate
}

type DepositQuoteRate struct {
	AssetID      string
	USDPerCoin   string `json:",optional"`
	EstimatedGas string
}

const depositPreviewTTL = 30 * time.Second
const testSheetSource = "test_sheet"

var rateSyntax = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)
var versionSyntax = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type depositAsset struct {
	network       Network
	asset, native Asset
	rate, gas     decimal.Decimal
}

type depositBook struct {
	solReviewAfter map[string]time.Duration
	ton            map[string]*tonDepositRoute
	tron           map[string]*nileDepositRoute
	base           map[string]*baseDepositRoute
	market         *DepositMarketConfig
	solana         map[string]*solanaDepositRoute
	version        string
	asOf, until    int64
	assets         map[string]depositAsset
}

func newDepositBook(c *DepositQuoteConfig, networks []Network, testMint, tonMaster string) (depositBook, error) {
	var book depositBook
	if c == nil {
		return book, nil
	}
	if !versionSyntax.MatchString(c.Version) || len(c.Assets) == 0 {
		return book, fmt.Errorf("invalid deposit quote version or empty asset list")
	}
	if c.Market == nil {
		if c.AsOf <= 0 || c.ValidUntil <= c.AsOf {
			return book, fmt.Errorf("invalid static deposit quote time window")
		}
	} else if c.Market.Source != marketdata.Kraken || c.Market.MaxAge < time.Second || c.AsOf != 0 || c.ValidUntil != 0 {
		return book, fmt.Errorf("exchange rates require a supported source, MaxAge of at least one second and no static time window")
	}
	if c.SolanaSOLReviewAfter != 0 && (c.SolanaSOL == nil || c.SolanaSOLReviewAfter < time.Second) {
		return book, fmt.Errorf("SOL authorization requires a Devnet SOL wallet route and a positive review interval of at least one second")
	}
	if c.SolanaUSDCReviewAfter != 0 && (c.SolanaUSDC == nil || c.SolanaUSDCReviewAfter < time.Second) {
		return book, fmt.Errorf("USDC authorization requires a Devnet USDC route and review interval of at least one second")
	}
	book = depositBook{solReviewAfter: map[string]time.Duration{solanaDepositAsset: c.SolanaSOLReviewAfter, solanaDepositUSDC: c.SolanaUSDCReviewAfter}, market: c.Market, version: c.Version, asOf: c.AsOf, until: c.ValidUntil, assets: make(map[string]depositAsset)}
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
		if c.Market == nil {
			if !rateSyntax.MatchString(row.USDPerCoin) {
				return book, fmt.Errorf("invalid USD rate for %s", row.AssetID)
			}
			entry.rate, _ = decimal.NewFromString(row.USDPerCoin)
			if !entry.rate.IsPositive() {
				return book, fmt.Errorf("zero USD rate for %s", row.AssetID)
			}
		} else if row.USDPerCoin != "" {
			return book, fmt.Errorf("static USD rate for %s conflicts with market source", row.AssetID)
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
	tonTokenID := asset(TONTestnet, "USDT", tonMaster, tonJettonDecimals, TestFixture).ID
	book.ton = make(map[string]*tonDepositRoute)
	for id, config := range map[string]*TONDepositFeeConfig{tonDepositAsset: c.TONNative, tonTokenID: c.TONTestUSDT} {
		if config == nil {
			continue
		}
		entry, ok := book.assets[id]
		if !ok || !entry.network.Enabled {
			return book, fmt.Errorf("TON preview requires the matching testnet asset, endpoint and rates for %s", id)
		}
		if id == tonTokenID && !sameAddress(entry.network, tonMaster, tonDepositMaster) {
			return book, fmt.Errorf("TON USDT preview requires the verified testnet master")
		}
		route, err := newTONDepositRoute(*config, entry.network)
		if err != nil {
			return book, err
		}
		book.ton[id] = route
	}
	book.tron = make(map[string]*nileDepositRoute)
	for id, config := range map[string]*TRONDepositFeeConfig{TRONNileTRX: c.TRONTRX, nileDepositUSDTID: c.TRONTestUSDT} {
		if config == nil {
			continue
		}
		entry, ok := book.assets[id]
		if !ok || !entry.network.Enabled {
			return book, fmt.Errorf("wallet fee preview for %s requires the matching Nile asset, endpoint and prices", id)
		}
		route, err := newNileDepositRoute(*config)
		if err != nil {
			return book, err
		}
		book.tron[id] = route
	}
	book.base = make(map[string]*baseDepositRoute)
	for id, config := range map[string]*BaseDepositFeeConfig{BaseSepoliaETH: c.BaseETH, baseusdc.AssetID: c.BaseUSDC} {
		if config == nil {
			continue
		}
		entry, ok := book.assets[id]
		if !ok || !entry.network.Enabled {
			return book, fmt.Errorf("wallet fee preview for %s requires a Base Sepolia endpoint and configured prices", id)
		}
		route, err := newBaseDepositRoute(id, *config)
		if err != nil {
			return book, err
		}
		book.base[id] = route
	}
	book.solana = make(map[string]*solanaDepositRoute)
	routes := map[string]*SolanaDepositFeeConfig{solanaDepositAsset: c.SolanaSOL, solanaDepositUSDC: c.SolanaUSDC}
	if c.SolanaTestUSDT != nil {
		if testMint == "" || testMint == catalog[solanaDepositUSDC].asset.Contract {
			return book, fmt.Errorf("Solana test USDT wallet fee preview requires a dedicated Devnet TestUSDTContract")
		}
		routes[solanaDepositUSDTPrefix+testMint] = c.SolanaTestUSDT
	}
	for id, config := range routes {
		if config == nil {
			continue
		}
		entry, ok := book.assets[id]
		if !ok || !entry.network.Enabled {
			return book, fmt.Errorf("wallet fee preview for %s requires a Devnet endpoint and a configured asset", id)
		}
		route, err := newSolanaDepositRoute(*config)
		if err != nil {
			return book, err
		}
		book.solana[id] = route
	}
	return book, nil
}

type DepositPreview struct {
	SolOrderIdx, SolMessage         string
	SolLastValidHeight, SolReviewAt int64
	QuoteIdx, MaxGas                string
	// Server-side terms retained with a wallet quote for subsequent order checks.
	Recipient, FeePayer, GasUSDPerCoin, RuleVersion  string
	PayDecimals, GasDecimals                         int
	EmulatedMessageHash, RelayAttached, RelaySurplus string
	JettonExcess                                     string
	DelegatedEnergy                                  int64
	DelegatedBandwidth                               int
	AssetID, TargetUSD, USDPerCoin, PayQuantity      string
	GasAssetID, EstimatedGas, GasUSD, NetUSD         string
	AccountFunding, AccountFundingUSD                string
	WalletFeePreview                                 bool
	WalletAddress, MessageSHA256, FeeBlockHash       string
	Version, RateSource                              string
	AsOf, QuotedAt, ExpiresAt                        int64
}

func (s *Service) PreviewWalletDeposit(ctx context.Context, user int64, assetID, targetUSD, address string) (DepositPreview, error) {
	if assetID == tonDepositAsset || s.quotes.ton[assetID] != nil {
		return s.previewTONDeposit(ctx, user, assetID, targetUSD, address, time.Now)
	}
	if assetID == TRONNileTRX || assetID == nileDepositUSDTID {
		return s.previewNileDeposit(ctx, user, assetID, targetUSD, address)
	}
	if assetID == BaseSepoliaETH || assetID == baseusdc.AssetID {
		return s.previewBaseDeposit(ctx, user, assetID, targetUSD, address)
	}
	return s.previewSolanaDeposit(ctx, user, assetID, targetUSD, address)
}

func (s *Service) PreviewDeposit(assetID, targetUSD string, now time.Time) (DepositPreview, error) {
	return s.PreviewDepositContext(context.Background(), assetID, targetUSD, now)
}

func (s *Service) PreviewDepositContext(ctx context.Context, assetID, targetUSD string, now time.Time) (DepositPreview, error) {
	prices, err := s.depositPrices(ctx, assetID, now)
	if err != nil {
		return DepositPreview{}, err
	}
	return s.depositQuote(assetID, targetUSD, s.quotes.assets[assetID].gas, now, prices)
}

func (s *Service) depositQuote(assetID, targetUSD string, gas decimal.Decimal, now time.Time, prices depositPrices) (DepositPreview, error) {
	var empty DepositPreview
	usd, _ := account.Lookup(account.KindUSD)
	if !rateSyntax.MatchString(targetUSD) {
		return empty, badTarget("invalid target USD amount")
	}
	target, _ := decimal.NewFromString(targetUSD)
	if !target.IsPositive() || target.Exponent() < -usd.Scale {
		return empty, badTarget("target USD must be a positive amount in cents")
	}
	entry := s.quotes.assets[assetID]
	gasUSD := gas.Mul(prices.native)
	scale := int32(entry.asset.Decimals)
	quantity, remainder := target.Add(gasUSD).QuoRem(prices.pay, scale)
	if !remainder.IsZero() {
		quantity = quantity.Add(decimal.New(1, -scale))
	}
	pay, err := chainQuantity(entry.network, entry.asset, quantity.String())
	if err != nil {
		return empty, err
	}
	net := quantity.Mul(prices.pay).Sub(gasUSD).Truncate(usd.Scale)
	if err := usd.ValidateBalances(net, decimal.Zero); err != nil {
		return empty, badTarget("net USD exceeds account capacity")
	}
	expires := now.Add(depositPreviewTTL)
	if expires.After(prices.until) {
		expires = prices.until
	}
	return DepositPreview{
		RuleVersion: s.quotes.version, GasUSDPerCoin: prices.native.String(), PayDecimals: entry.asset.Decimals, GasDecimals: entry.native.Decimals,
		AssetID: assetID, TargetUSD: target.StringFixed(usd.Scale), USDPerCoin: prices.pay.String(), PayQuantity: pay,
		GasAssetID: entry.native.ID, EstimatedGas: gas.String(), GasUSD: gasUSD.String(), NetUSD: net.StringFixed(usd.Scale),
		WalletFeePreview: s.quotes.ton[assetID] != nil || s.quotes.solana[assetID] != nil || s.quotes.base[assetID] != nil || s.quotes.tron[assetID] != nil,
		Version:          prices.version, RateSource: prices.source, AsOf: prices.asOf.UnixMilli(), QuotedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(),
	}, nil
}
