package rewardtransfer

import "testing"

func TestExactUSDInput(t *testing.T) {
	for _, v := range []string{"2", "2.00", "2.01", "999999999999999999.99"} {
		if _, err := ParseAmount(v); err != nil {
			t.Fatal(v, err)
		}
	}
	for _, v := range []string{"", "1.99", "0", "-2", "2.001", "2e3", "02", " 2", "1000000000000000000"} {
		if _, err := ParseAmount(v); err == nil {
			t.Fatal("accepted invalid amount", v)
		}
	}
}
