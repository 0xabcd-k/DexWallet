package chainassets

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/shopspring/decimal"
)

// ResourceOwner is the platform account that already delegated Bandwidth.
type TRONDepositFeeConfig struct{ ResourceOwner, Recipient string }

type nileDepositRoute struct {
	recipient, recipientHex, ownerHex string
}

func newNileDepositRoute(c TRONDepositFeeConfig) (*nileDepositRoute, error) {
	owner, err := tronnative.AddressBytes(c.ResourceOwner)
	if err != nil {
		return nil, fmt.Errorf("invalid Nile bandwidth owner: %w", err)
	}
	recipient, err := tronnative.AddressBytes(c.Recipient)
	if err != nil {
		return nil, fmt.Errorf("invalid Nile TRX recipient: %w", err)
	}
	return &nileDepositRoute{recipient: c.Recipient, recipientHex: hex.EncodeToString(recipient), ownerHex: hex.EncodeToString(owner)}, nil
}

type nileQuoteAccount struct {
	Address, Type string
	Balance       int64
	Owner         struct {
		ID, Threshold int64
		Keys          []struct {
			Address string
			Weight  int64
		}
	} `json:"owner_permission"`
	Frozen []struct {
		Balance int64 `json:"frozen_balance"`
	}
	FrozenV2 []struct {
		Type   string
		Amount int64
	}
	LegacyIncoming int64 `json:"acquired_delegated_frozen_balance_for_bandwidth"`
	Incoming       int64 `json:"acquired_delegated_frozenV2_balance_for_bandwidth"`
}

func (a nileQuoteAccount) hasOtherBandwidth() bool {
	if a.LegacyIncoming != 0 {
		return true
	}
	for _, f := range a.Frozen {
		if f.Balance != 0 {
			return true
		}
	}
	for _, f := range a.FrozenV2 {
		if (f.Type == "" || f.Type == "BANDWIDTH") && f.Amount != 0 {
			return true
		}
	}
	return false
}

func (s *Service) previewNileTRX(ctx context.Context, user int64, target, address string) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.tronTRX
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile TRX wallet fee preview disabled"))
	}
	wallet, err := tronnative.AddressBytes(address)
	walletHex := hex.EncodeToString(wallet)
	if err != nil || address == route.recipient || walletHex == route.ownerHex {
		return empty, badTarget("invalid Nile TRX deposit source")
	}
	start := time.Now()
	prices, err := s.depositPrices(ctx, TRONNileTRX, start)
	if err != nil {
		return empty, err
	}
	q, err := s.depositQuote(TRONNileTRX, target, decimal.Zero, start, prices)
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
	n, e, err := s.nileFull(ctx)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	head, err := s.nileHeadAt(ctx, n, e)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	now := time.Now().UnixMilli()
	if head.Timestamp > now || now-head.Timestamp >= depositPreviewTTL.Milliseconds() {
		return empty, apperror.New(apperror.ServiceUnavailable, invalid("invalid or stale Nile fee reference"))
	}
	q.ExpiresAt = min(q.ExpiresAt, head.Timestamp+depositPreviewTTL.Milliseconds())
	pay, _ := decimal.NewFromString(q.PayQuantity)
	sun := pay.Shift(int32(s.quotes.assets[TRONNileTRX].asset.Decimals)).IntPart()
	tx := tronnative.Transfer{Sender: address, Recipient: route.recipient, Sun: sun, Reference: head, Expiration: head.Timestamp + tronnative.Lifetime.Milliseconds()}
	raw, err := tx.Raw()
	if err != nil {
		return empty, err
	}
	required := tronnative.SingleSignatureBandwidth(raw)
	if err = s.checkNileDepositResources(ctx, n, e, walletHex, route, sun, required); err != nil {
		return empty, err
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile deposit estimate expired"))
	}
	q.WalletAddress, q.MessageSHA256, q.DelegatedBandwidth = address, tronnative.TxID(raw), required
	return q, nil
}

func (s *Service) checkNileDepositResources(ctx context.Context, n Network, e Endpoint, wallet string, route *nileDepositRoute, sun int64, required int) error {
	read := func(method string, body map[string]any, out any) error {
		var raw json.RawMessage
		if err := s.tronObject(ctx, n, e, "/wallet/"+method, body, &raw); err != nil {
			return apperror.New(apperror.ServiceUnavailable, err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return apperror.New(apperror.ServiceUnavailable, invalid("malformed Nile "+method+" response"))
		}
		return nil
	}
	var source, payee nileQuoteAccount
	if err := read("getaccount", map[string]any{"address": wallet}, &source); err != nil {
		return err
	}
	if source.Address == "" {
		return badTarget("inactive Nile source")
	}
	if !strings.EqualFold(source.Address, wallet) || source.Balance < 0 {
		return apperror.New(apperror.ServiceUnavailable, invalid("incorrect Nile source account response"))
	}
	p := source.Owner
	if (source.Type != "" && source.Type != "Normal") || p.ID != 0 || p.Threshold <= 0 || len(p.Keys) != 1 || !strings.EqualFold(p.Keys[0].Address, wallet) || p.Keys[0].Weight < p.Threshold {
		return badTarget("Nile preview requires a single owner signing key")
	}
	if source.Balance < sun {
		return apperror.New(apperror.InsufficientBalance, nil)
	}
	if err := read("getaccount", map[string]any{"address": route.recipientHex}, &payee); err != nil {
		return err
	}
	if !strings.EqualFold(payee.Address, route.recipientHex) || (payee.Type != "" && payee.Type != "Normal") {
		return apperror.New(apperror.ServiceUnavailable, invalid("Nile TRX deposit recipient must be an active ordinary account"))
	}
	if source.hasOtherBandwidth() || source.Incoming <= 0 {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile bandwidth not exclusively platform-delegated"))
	}
	var grants struct {
		DelegatedResource []struct {
			From, To string
			Stake    int64 `json:"frozen_balance_for_bandwidth"`
		}
	}
	if err := read("getdelegatedresourcev2", map[string]any{"fromAddress": route.ownerHex, "toAddress": wallet}, &grants); err != nil {
		return err
	}
	remaining := source.Incoming
	for _, g := range grants.DelegatedResource {
		if !strings.EqualFold(g.From, route.ownerHex) || !strings.EqualFold(g.To, wallet) || g.Stake < 0 || g.Stake > remaining {
			return apperror.New(apperror.ServiceUnavailable, invalid("inconsistent Nile bandwidth delegation"))
		}
		remaining -= g.Stake
	}
	if remaining != 0 {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform does not supply the full Nile bandwidth stake"))
	}
	var resources struct{ NetLimit, NetUsed int64 }
	if err := read("getaccountresource", map[string]any{"address": wallet}, &resources); err != nil {
		return err
	}
	if resources.NetUsed < 0 || resources.NetLimit < resources.NetUsed || resources.NetLimit-resources.NetUsed < int64(required) {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("insufficient delegated Nile bandwidth for %d points", required))
	}
	return nil
}
