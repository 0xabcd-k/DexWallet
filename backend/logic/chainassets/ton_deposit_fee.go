package chainassets

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/shopspring/decimal"
)

const (
	tonDepositAsset      = TONTestnet + ":ton"
	tonTestnetGlobalID   = -3
	tonRelayNano         = 3_000_000 // Platform attachment used by the verified W5 native relay.
	tonRelayValidity     = 5 * time.Minute
	maxTONFeeSimulations = 3
)

// PlatformWallet is both the dispatcher and the final recipient of the native payment.
type TONDepositFeeConfig struct{ PlatformWallet, TonAPIURL string }
type tonDepositRoute struct {
	platform string
	emulator Endpoint
}

func newTONDepositRoute(c TONDepositFeeConfig, n Network) (*tonDepositRoute, error) {
	platform, err := normalizeAddress(n, c.PlatformWallet)
	if err != nil || !strings.HasPrefix(platform, "0:") || platform == "0:"+strings.Repeat("0", 64) {
		return nil, fmt.Errorf("invalid TON deposit platform wallet")
	}
	endpoint := Endpoint{Network: TONTestnet, URL: c.TonAPIURL}
	if err := (Config{Networks: []Endpoint{endpoint}}).Check(); err != nil {
		return nil, fmt.Errorf("TON deposit emulation endpoint: %w", err)
	}
	return &tonDepositRoute{platform, endpoint}, nil
}

func (s *Service) previewTONDeposit(ctx context.Context, user int64, target, walletAddress string, now func() time.Time) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.ton
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON wallet fee preview disabled"))
	}
	n := s.quotes.assets[tonDepositAsset].network
	wallet, err := normalizeAddress(n, walletAddress)
	if err != nil || !strings.HasPrefix(wallet, "0:") || wallet == route.platform || wallet == "0:"+strings.Repeat("0", 64) {
		return empty, badTarget("invalid TON deposit source")
	}
	start := now()
	prices, err := s.depositPrices(ctx, tonDepositAsset, start)
	if err != nil {
		return empty, err
	}
	q, err := s.depositQuote(tonDepositAsset, target, decimal.Zero, start, prices)
	if err != nil {
		return empty, err
	}
	if s.limiter == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query limiter not configured"))
	}
	if err = s.limiter.Allow(ctx, user); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var head struct {
		GlobalID  int   `json:"global_id"`
		Workchain *int  `json:"workchain_id"`
		Time      int64 `json:"gen_utime"`
		Seqno     int64
	}
	if err = s.request(ctx, n, route.emulator, "/v2/blockchain/masterchain-head", nil, nil, &head); err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	at := time.Unix(head.Time, 0)
	age := now().Sub(at)
	if head.GlobalID != tonTestnetGlobalID || head.Workchain == nil || *head.Workchain != -1 || head.Seqno <= 0 || age < 0 || age >= depositPreviewTTL {
		return empty, apperror.New(apperror.ServiceUnavailable, invalid("incorrect or stale TON testnet head"))
	}
	source, err := s.tonDepositWallet(ctx, n, route.emulator, wallet)
	if err != nil {
		return empty, err
	}
	platform, err := s.tonDepositWallet(ctx, n, route.emulator, route.platform)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON deposit platform wallet: %w", err))
	}
	attached := decimal.New(tonRelayNano, -9)
	if platform.balance.LessThan(attached) {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform cannot fund TON relay"))
	}
	// The transfer includes its own estimated fee. Simulate the final quantity too:
	// changing its encoded byte length can change forwarding fees.
	for attempt := 0; attempt < maxTONFeeSimulations; attempt++ {
		pay, _ := decimal.NewFromString(q.PayQuantity)
		if source.balance.LessThan(pay) {
			return empty, apperror.New(apperror.InsufficientBalance, nil)
		}
		message, err := buildTONDepositMessage(source, platform, pay.Shift(9).BigInt(), uint32(at.Add(tonRelayValidity).Unix()))
		if err != nil {
			return empty, err
		}
		var trace tonDepositTrace
		err = s.request(ctx, n, route.emulator, "/v2/traces/emulate", url.Values{"ignore_signature_check": {"true"}}, map[string]string{"boc": base64.StdEncoding.EncodeToString(message.external.ToBOC())}, &trace)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
		budget, err := trace.budget(message, source, platform, pay, at)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
		if platform.balance.LessThan(attached.Add(budget.dispatch)) {
			return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform TON balance cannot cover dispatch and relay attachment"))
		}
		next, err := s.depositQuote(tonDepositAsset, target, budget.network, start, prices)
		if err != nil {
			return empty, err
		}
		if next.PayQuantity == q.PayQuantity {
			next.ExpiresAt = min(next.ExpiresAt, at.Add(depositPreviewTTL).UnixMilli())
			if now().UnixMilli() >= next.ExpiresAt {
				return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON estimate expired"))
			}
			next.WalletAddress = walletAddress
			next.EmulatedMessageHash = hex.EncodeToString(message.external.Hash())
			next.RelayAttached, next.RelaySurplus = attached.String(), attached.Sub(budget.execution).String()
			return next, nil
		}
		q = next
	}
	return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON fee changed during deposit estimate"))
}
