// Package draw settles points draws; the client only presents committed results.
package draw

import (
	"encoding/json"
	"fmt"

	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/shopspring/decimal"
)

const (
	CostPoints  int64 = 100
	TotalWeight int64 = 10000
	PriceCents  int64 = 10
	PrizeCount        = 7
	PrizeOne          = "ordinary_one"
	PrizeTwo          = "ordinary_two"
	PrizeThree        = "ordinary_three"
	PrizeSmall        = "small"
	PrizeDouble       = "double"
	PrizeLarge        = "large"
	PrizeTop          = "top"
)

type Prize struct {
	Code      string          `json:"code"`
	RewardUSD decimal.Decimal `json:"rewardUsd"`
	Weight    int64           `json:"weight"`
	Symbols   [3]int64        `json:"symbols"`
}
type Rules struct {
	RTP    decimal.Decimal `json:"rtp"`
	Prizes []Prize         `json:"prizes"`
}

// SampleRules is the documented candidate, for explicitly isolated development.
// It is not inserted by migrations or automatically enabled in production.
func SampleRules() Rules {
	codes := []string{PrizeOne, PrizeTwo, PrizeThree, PrizeSmall, PrizeDouble, PrizeLarge, PrizeTop}
	cents := []int64{8, 9, 10, 12, 20, 100, 500}
	weights := []int64{1500, 5000, 3000, 400, 90, 9, 1}
	symbols := [][3]int64{{1, 5, 6}, {1, 1, 5}, {1, 1, 1}, {2, 2, 2}, {3, 3, 3}, {4, 4, 4}, {7, 7, 7}}
	r := Rules{RTP: decimal.New(95, -2)}
	for i, code := range codes {
		r.Prizes = append(r.Prizes, Prize{code, decimal.New(cents[i], -2), weights[i], symbols[i]})
	}
	return r
}

func (r Rules) Validate() error {
	if len(r.Prizes) != PrizeCount || !r.RTP.IsPositive() {
		return fmt.Errorf("invalid prize count or RTP")
	}
	seen := map[string]bool{}
	weight := int64(0)
	expected := decimal.Zero
	definition, _ := accounts.Lookup(accounts.KindRewardUSD)
	for _, p := range r.Prizes {
		if seen[p.Code] || p.Weight < 0 || p.Weight > TotalWeight || !p.RewardUSD.IsPositive() {
			return fmt.Errorf("invalid prize %s", p.Code)
		}
		if err := definition.ValidateBalances(p.RewardUSD, decimal.Zero); err != nil {
			return err
		}
		if code, err := SymbolPrize(p.Symbols); err != nil || code != p.Code {
			return fmt.Errorf("prize %s has mismatched symbols", p.Code)
		}
		seen[p.Code] = true
		weight += p.Weight
		expected = expected.Add(p.RewardUSD.Mul(decimal.NewFromInt(p.Weight)))
	}
	// Multiplication is exact; no floating-point probability or rounded RTP check.
	want := r.RTP.Mul(decimal.New(PriceCents, -2)).Mul(decimal.NewFromInt(TotalWeight))
	if weight != TotalWeight || !expected.Equal(want) {
		return fmt.Errorf("weights or exact expected return do not match RTP")
	}
	return nil
}

func SymbolPrize(s [3]int64) (string, error) {
	ones := 0
	for _, v := range s {
		if v < 1 || v > 7 {
			return "", fmt.Errorf("invalid symbol")
		}
		if v == 1 {
			ones++
		}
	}
	switch ones {
	case 1:
		return PrizeOne, nil
	case 2:
		return PrizeTwo, nil
	case 3:
		return PrizeThree, nil
	}
	if s[0] == s[1] && s[1] == s[2] {
		switch s[0] {
		case 2:
			return PrizeSmall, nil
		case 3:
			return PrizeDouble, nil
		case 4:
			return PrizeLarge, nil
		case 7:
			return PrizeTop, nil
		}
	}
	return "", fmt.Errorf("symbols do not match a prize")
}

func (r Rules) Pick(ticket int64) (Prize, error) {
	if ticket < 0 || ticket >= TotalWeight {
		return Prize{}, fmt.Errorf("random ticket outside range")
	}
	for _, p := range r.Prizes {
		if ticket < p.Weight {
			return p, nil
		}
		ticket -= p.Weight
	}
	return Prize{}, fmt.Errorf("invalid prize weights")
}
func ParseRules(raw string) (Rules, error) {
	var r Rules
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}
