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
	tonDepositAsset          = TONTestnet + ":ton"
	tonTestnetGlobalID       = -3
	TONDepositAttachmentNano = 3_000_000 // Platform attachment used by the verified W5 native relay.
	tonRelayValidity         = 5 * time.Minute
	maxTONFeeSimulations     = 3
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

func (s *Service) previewTONDeposit(ctx context.Context, user int64, assetID, target, walletAddress string, now func() time.Time) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.ton[assetID]
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON wallet fee preview disabled"))
	}
	entry := s.quotes.assets[assetID]
	n := entry.network
	wallet, err := normalizeAddress(n, walletAddress)
	if err != nil || !strings.HasPrefix(wallet, "0:") || wallet == route.platform || wallet == "0:"+strings.Repeat("0", 64) {
		return empty, badTarget("invalid TON deposit source")
	}
	start := now()
	prices, err := s.depositPrices(ctx, assetID, start)
	if err != nil {
		return empty, err
	}
	q, err := s.depositQuote(assetID, target, decimal.Zero, start, prices)
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
	at, err := s.tonDepositHead(ctx, n, route.emulator, now)
	if err != nil {
		return empty, err
	}
	source, err := s.tonDepositWallet(ctx, n, route.emulator, wallet)
	if err != nil {
		return empty, err
	}
	platform, err := s.tonDepositWallet(ctx, n, route.emulator, route.platform)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON deposit platform wallet: %w", err))
	}
	var storageAllowance decimal.Decimal
	if assetID == tonDepositAsset && s.quotes.tonNativeExecution {
		storageAllowance, err = s.tonStorageAllowance(ctx, n, route.emulator, source, platform, at)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
	}
	var token *tonDepositJetton
	balance := source.balance
	attachmentNano := int64(TONDepositAttachmentNano)
	if assetID != tonDepositAsset {
		token, err = s.tonDepositJetton(ctx, n, route.emulator, source, platform)
		if err != nil {
			return empty, err
		}
		balance = token.balance
		attachmentNano += tonJettonActionNano
	}
	attached := decimal.New(attachmentNano, -9)
	if platform.balance.LessThan(attached) {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform cannot fund TON relay"))
	}
	// The transfer includes its own estimated fee. Simulate the final quantity too:
	// changing its encoded byte length can change forwarding fees.
	for attempt := 0; attempt < maxTONFeeSimulations; attempt++ {
		pay, _ := decimal.NewFromString(q.PayQuantity)
		if balance.LessThan(pay) {
			return empty, apperror.New(apperror.InsufficientBalance, nil)
		}
		message, err := buildTONDepositMessage(source, platform, token, pay.Shift(int32(entry.asset.Decimals)).BigInt(), uint32(at.Add(tonRelayValidity).Unix()))
		if err != nil {
			return empty, err
		}
		var trace tonDepositTrace
		err = s.request(ctx, n, route.emulator, "/v2/traces/emulate", url.Values{"ignore_signature_check": {"true"}}, map[string]string{"boc": base64.StdEncoding.EncodeToString(message.external.ToBOC())}, &trace)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
		budget, err := trace.budget(message, source, platform, token, pay, at)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
		if platform.balance.LessThan(attached.Add(budget.dispatch)) {
			return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform TON balance cannot cover dispatch and relay attachment"))
		}
		next, err := s.depositQuote(assetID, target, budget.network.Add(storageAllowance), start, prices)
		if err != nil {
			return empty, err
		}
		if next.PayQuantity == q.PayQuantity {
			next.ExpiresAt = min(next.ExpiresAt, at.Add(depositPreviewTTL).UnixMilli())
			if now().UnixMilli() >= next.ExpiresAt {
				return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON estimate expired"))
			}
			if token == nil && s.quotes.tonNativeExecution {
				next.TON = &TONDepositTerms{SimulatedGas: budget.network.String(), UserKey: hex.EncodeToString(source.key), PlatformKey: hex.EncodeToString(platform.key), UserSeqno: uint32(source.seqno), PlatformSeqno: uint32(platform.seqno), ValidUntil: uint32(at.Add(tonRelayValidity).Unix()), AuthorizationHash: hex.EncodeToString(message.userPayload.Hash())}
			}
			next.Recipient, next.FeePayer = route.platform, route.platform
			next.WalletAddress = walletAddress
			next.EmulatedMessageHash = hex.EncodeToString(message.external.Hash())
			next.RelayAttached, next.RelaySurplus = attached.String(), decimal.New(TONDepositAttachmentNano, -9).Sub(budget.execution).String()
			if token != nil {
				next.JettonExcess = budget.excess.String()
				next.AccountFunding = budget.reserve.String()
				next.AccountFundingUSD = budget.reserve.Mul(prices.native).String()
			}
			return next, nil
		}
		q = next
	}
	return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("TON fee changed during deposit estimate"))
}

func (s *Service) tonDepositHead(ctx context.Context, n Network, endpoint Endpoint, now func() time.Time) (time.Time, error) {
	var head struct {
		GlobalID  int   `json:"global_id"`
		Workchain *int  `json:"workchain_id"`
		Time      int64 `json:"gen_utime"`
		Seqno     int64
	}
	if err := s.request(ctx, n, endpoint, "/v2/blockchain/masterchain-head", nil, nil, &head); err != nil {
		return time.Time{}, apperror.New(apperror.ServiceUnavailable, err)
	}
	at := time.Unix(head.Time, 0)
	age := now().Sub(at)
	if head.GlobalID != tonTestnetGlobalID || head.Workchain == nil || *head.Workchain != -1 || head.Seqno <= 0 || age < 0 || age >= depositPreviewTTL {
		return time.Time{}, apperror.New(apperror.ServiceUnavailable, invalid("incorrect or stale TON testnet head"))
	}
	return at, nil
}
