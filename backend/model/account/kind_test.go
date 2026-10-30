package account

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestBalancePrecisionAndRange(t *testing.T) {
	for _, tc := range []struct {
		name, kind, balance, held string
		valid                     bool
	}{
		{"zero", KindUSD, "0", "0", true},
		{"cents", KindUSD, "9007199254740993.01", "2.09", true},
		{"maximum", KindUSD, "999999999999999999.99", "0", true},
		{"maximum split", KindUSD, "999999999999999999.98", "0.01", true},
		{"release would overflow", KindUSD, "999999999999999999.99", "0.01", false},
		{"trailing zeroes", KindRewardUSD, "1.2300", "0", true},
		{"integer points", KindTaskPoints, "999999999999999999", "0", true},
		{"extra cents", KindUSD, "1.001", "0", false},
		{"tiny amount", KindUSD, "0.000000000000000001", "0", false},
		{"fractional points", KindTaskPoints, "1.1", "0", false},
		{"negative", KindUSD, "-1", "0", false},
		{"negative held", KindUSD, "0", "-1", false},
		{"overflow", KindUSD, "1000000000000000000", "0", false},
		{"held precision", KindUSD, "1", "0.001", false},
		{"reward hold", KindRewardUSD, "1", "1", false},
		{"points hold", KindTaskPoints, "1", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := Lookup(tc.kind)
			if !ok {
				t.Fatal("missing definition")
			}
			err := d.ValidateBalances(decimal.RequireFromString(tc.balance), decimal.RequireFromString(tc.held))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t: %v", tc.valid, err)
			}
		})
	}
	if _, ok := Lookup("unknown"); ok {
		t.Fatal("unknown account accepted")
	}
}
