package chainassets

import (
	"context"
	"math/big"
	"time"

	"github.com/shopspring/decimal"
)

// Config 18 prices basechain cells and bits per 65,536 seconds. Account rent
// may advance between emulation and inclusion; use current W5 sizes and prices
// through the signed deadline for the displayed maximum, never for settlement.
type tonStorageUsed struct {
	Cells *int64 `json:"used_cells"`
	Bits  *int64 `json:"used_bits"`
}
type tonStoragePrice struct {
	Since *int64 `json:"utime_since"`
	Bit   *int64 `json:"bit_price_ps"`
	Cell  *int64 `json:"cell_price_ps"`
}

func (s *Service) tonStorageAllowance(ctx context.Context, n Network, e Endpoint, source, platform tonW5Account, at time.Time, jettons ...*tonStorageUsed) (decimal.Decimal, error) {
	var cfg struct {
		Storage struct {
			Prices []tonStoragePrice `json:"storage_prices"`
		} `json:"18"`
	}
	if err := s.request(ctx, n, e, "/v2/blockchain/config", nil, nil, &cfg); err != nil {
		return decimal.Zero, err
	}
	return tonRelayStorageCost(cfg.Storage.Prices, source.storage, platform.storage, at.Unix(), at.Add(tonRelayValidity).Unix(), jettons...)
}
func tonRelayStorageCost(prices []tonStoragePrice, user, sponsor *tonStorageUsed, start, end int64, jettons ...*tonStorageUsed) (decimal.Decimal, error) {
	var last int64 = -1
	var active bool
	var bit, cell int64
	for _, p := range prices {
		if p.Since == nil || p.Bit == nil || p.Cell == nil || *p.Bit < 0 || *p.Cell < 0 || *p.Since <= last {
			return decimal.Zero, invalid("invalid TON storage price schedule")
		}
		last = *p.Since
		if last <= start {
			bit, cell, active = *p.Bit, *p.Cell, true
		} else if last <= end {
			bit = max(bit, *p.Bit)
			cell = max(cell, *p.Cell)
		}
	}
	if !active || end <= start {
		return decimal.Zero, invalid("missing current TON storage prices")
	}
	total := new(big.Int)
	for _, a := range append([]*tonStorageUsed{user, sponsor}, jettons...) {
		if a == nil || a.Bits == nil || a.Cells == nil || *a.Bits <= 0 || *a.Cells <= 0 {
			return decimal.Zero, invalid("missing W5 storage usage")
		}
		units := new(big.Int).Mul(big.NewInt(*a.Bits), big.NewInt(bit))
		units.Add(units, new(big.Int).Mul(big.NewInt(*a.Cells), big.NewInt(cell)))
		units.Mul(units, big.NewInt(end-start))
		units.Add(units, big.NewInt(65535)).Rsh(units, 16)
		total.Add(total, units)
	}
	// The platform pays storage in two transactions, each rounded separately.
	if bit > 0 || cell > 0 {
		rounding := int64(1)
		// Notification and excess are separate platform transactions.
		if len(jettons) > 0 {
			rounding++
		}
		total.Add(total, big.NewInt(rounding))
	}
	return decimal.NewFromBigInt(total, -9), nil
}
