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

// ResourceOwner is the platform account that delegated the resources for this route.
type TRONDepositFeeConfig struct{ ResourceOwner, Recipient string }

type nileDepositRoute struct {
	recipient, recipientHex, ownerHex string
}

func newNileDepositRoute(c TRONDepositFeeConfig) (*nileDepositRoute, error) {
	owner, err := tronnative.AddressBytes(c.ResourceOwner)
	if err != nil {
		return nil, fmt.Errorf("invalid Nile resource owner: %w", err)
	}
	recipient, err := tronnative.AddressBytes(c.Recipient)
	if err != nil {
		return nil, fmt.Errorf("invalid Nile deposit recipient: %w", err)
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
	Energy         struct {
		Frozen struct {
			Balance int64 `json:"frozen_balance"`
		} `json:"frozen_balance_for_energy"`
		LegacyIncoming int64 `json:"acquired_delegated_frozen_balance_for_energy"`
		Incoming       int64 `json:"acquired_delegated_frozenV2_balance_for_energy"`
	} `json:"account_resource"`
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

func (s *Service) previewNileDeposit(ctx context.Context, user int64, assetID, target, address string) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.tron[assetID]
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile wallet fee preview disabled for %s", assetID))
	}
	wallet, err := tronnative.AddressBytes(address)
	walletHex := hex.EncodeToString(wallet)
	if err != nil || address == route.recipient || walletHex == route.ownerHex {
		return empty, badTarget("invalid Nile deposit source")
	}
	start := time.Now()
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
	asset := s.quotes.assets[assetID].asset
	atoms := pay.Shift(int32(asset.Decimals)).BigInt()
	tx := tronnative.Transfer{Sender: address, Recipient: route.recipient, Reference: head, Expiration: head.Timestamp + tronnative.Lifetime.Milliseconds()}
	if asset.Contract == "" {
		tx.Sun = atoms.Int64()
	} else {
		tx.TokenContract, tx.TokenAtoms, tx.FeeLimitSun = asset.Contract, atoms.String(), nileUSDTFeeLimitSun
	}
	raw, err := tx.Raw()
	if err != nil {
		return empty, err
	}
	required := tronnative.SingleSignatureBandwidth(raw)
	read := s.nileQuoteReader(ctx, n, e)
	q.DelegatedEnergy, err = estimateNileResources(read, walletHex, route, tx, required)
	if err != nil {
		return empty, err
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile deposit estimate expired"))
	}
	q.WalletAddress, q.MessageSHA256, q.DelegatedBandwidth = address, tronnative.TxID(raw), required
	return q, nil
}

type nileQuoteRead func(string, map[string]any, any) error

func (s *Service) nileQuoteReader(ctx context.Context, n Network, e Endpoint) nileQuoteRead {
	return func(method string, body map[string]any, out any) error {
		var raw json.RawMessage
		if err := s.tronObject(ctx, n, e, "/wallet/"+method, body, &raw); err != nil {
			return apperror.New(apperror.ServiceUnavailable, err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return apperror.New(apperror.ServiceUnavailable, invalid("malformed Nile "+method+" response"))
		}
		return nil
	}
}

func estimateNileResources(read nileQuoteRead, wallet string, route *nileDepositRoute, tx tronnative.Transfer, bandwidth int) (int64, error) {
	var source nileQuoteAccount
	if err := read("getaccount", map[string]any{"address": wallet}, &source); err != nil {
		return 0, err
	}
	if source.Address == "" {
		return 0, badTarget("inactive Nile source")
	}
	if !strings.EqualFold(source.Address, wallet) || source.Balance < 0 {
		return 0, apperror.New(apperror.ServiceUnavailable, invalid("incorrect Nile source account response"))
	}
	p := source.Owner
	if (source.Type != "" && source.Type != "Normal") || p.ID != 0 || p.Threshold <= 0 || len(p.Keys) != 1 || !strings.EqualFold(p.Keys[0].Address, wallet) || p.Keys[0].Weight < p.Threshold {
		return 0, badTarget("Nile preview requires a single owner signing key")
	}
	if source.Balance < tx.Sun {
		return 0, apperror.New(apperror.InsufficientBalance, nil)
	}
	var energy int64
	if tx.TokenContract == "" {
		var payee nileQuoteAccount
		if err := read("getaccount", map[string]any{"address": route.recipientHex}, &payee); err != nil {
			return 0, err
		}
		if !strings.EqualFold(payee.Address, route.recipientHex) || (payee.Type != "" && payee.Type != "Normal") {
			return 0, apperror.New(apperror.ServiceUnavailable, invalid("Nile TRX deposit recipient must be an active ordinary account"))
		}
	} else {
		var err error
		energy, err = estimateNileTokenEnergy(read, wallet, route.recipientHex, tx)
		if err != nil {
			return 0, err
		}
	}
	if err := checkNileDelegation(read, wallet, route.ownerHex, source, bandwidth, energy); err != nil {
		return 0, err
	}
	return energy, nil
}

func checkNileDelegation(read nileQuoteRead, wallet, owner string, source nileQuoteAccount, bandwidth int, energy int64) error {
	if source.hasOtherBandwidth() || source.Incoming <= 0 {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile bandwidth not exclusively platform-delegated"))
	}
	if energy > 0 {
		if source.Energy.Incoming <= 0 || source.Energy.LegacyIncoming != 0 || source.Energy.Frozen.Balance != 0 {
			return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile Energy not exclusively platform-delegated"))
		}
		for _, f := range source.FrozenV2 {
			if f.Type == "ENERGY" && f.Amount != 0 {
				return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Nile account has its own Energy stake"))
			}
		}
	}
	var grants struct {
		DelegatedResource []struct {
			From, To    string
			Stake       int64 `json:"frozen_balance_for_bandwidth"`
			EnergyStake int64 `json:"frozen_balance_for_energy"`
		}
	}
	if err := read("getdelegatedresourcev2", map[string]any{"fromAddress": owner, "toAddress": wallet}, &grants); err != nil {
		return err
	}
	remaining := source.Incoming
	remainingEnergy := source.Energy.Incoming
	for _, g := range grants.DelegatedResource {
		if !strings.EqualFold(g.From, owner) || !strings.EqualFold(g.To, wallet) || g.Stake < 0 || g.Stake > remaining {
			return apperror.New(apperror.ServiceUnavailable, invalid("inconsistent Nile bandwidth delegation"))
		}
		remaining -= g.Stake
		if energy > 0 {
			if g.EnergyStake < 0 || g.EnergyStake > remainingEnergy {
				return apperror.New(apperror.ServiceUnavailable, invalid("inconsistent Nile Energy delegation"))
			}
			remainingEnergy -= g.EnergyStake
		}
	}
	if remaining != 0 || (energy > 0 && remainingEnergy != 0) {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("platform does not supply the full Nile resource stake"))
	}
	var resources struct{ NetLimit, NetUsed, EnergyLimit, EnergyUsed int64 }
	if err := read("getaccountresource", map[string]any{"address": wallet}, &resources); err != nil {
		return err
	}
	if resources.NetUsed < 0 || resources.NetLimit < resources.NetUsed || resources.NetLimit-resources.NetUsed < int64(bandwidth) {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("insufficient delegated Nile bandwidth for %d points", bandwidth))
	}
	if energy > 0 && (resources.EnergyUsed < 0 || resources.EnergyLimit < resources.EnergyUsed || resources.EnergyLimit-resources.EnergyUsed < energy) {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("insufficient delegated Nile Energy for %d units", energy))
	}
	return nil
}
