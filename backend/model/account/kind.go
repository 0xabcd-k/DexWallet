package account

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Persisted codes and units are stable; display names belong to the client.
const (
	KindUSD        = "usd_balance"
	KindRewardUSD  = "reward_usd"
	KindTaskPoints = "task_points"
	UnitUSD        = "USD"
	UnitPoints     = "POINT"
)

type Definition struct {
	Kind      string
	Unit      string
	Scale     int32
	AllowHeld bool
}

// Definitions also defines the order of the account summary. Returning values
// keeps callers from changing the rules for other requests.
func Definitions() []Definition {
	return []Definition{
		{Kind: KindUSD, Unit: UnitUSD, Scale: 2, AllowHeld: true},
		{Kind: KindRewardUSD, Unit: UnitUSD, Scale: 2},
		{Kind: KindTaskPoints, Unit: UnitPoints, Scale: 0},
	}
}

func Lookup(kind string) (Definition, bool) {
	for _, definition := range Definitions() {
		if definition.Kind == kind {
			return definition, true
		}
	}
	return Definition{}, false
}

// DECIMAL(36,18) reserves 18 integer digits. Business precision may be lower.
var amountLimit = decimal.New(1, 18)

func (d Definition) ValidateBalances(balance, held decimal.Decimal) error {
	for _, amount := range []decimal.Decimal{balance, held} {
		if amount.IsNegative() || amount.GreaterThanOrEqual(amountLimit) {
			return fmt.Errorf("%s amount out of range: %s", d.Kind, amount)
		}
		if !amount.Equal(amount.Truncate(d.Scale)) {
			return fmt.Errorf("%s amount exceeds %d decimal places: %s", d.Kind, d.Scale, amount)
		}
	}
	if !d.AllowHeld && !held.IsZero() {
		return fmt.Errorf("%s does not support held balance", d.Kind)
	}
	return nil
}
