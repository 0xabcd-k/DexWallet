package deposit

import (
	"math/big"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestBaseUSDCCreditUsesActualNetworkFee(t *testing.T) {
	q := chainassets.DepositPreview{USDPerCoin: "0.98", GasUSDPerCoin: "4000", NetUSD: "0.01"}
	// 51,021 atoms are the rounded-up gross for a one-cent target and four cents of gas.
	cap := big.NewInt(10000000000000)
	for _, tc := range []struct {
		fee int64
		net string
	}{{10000000000000, "0.01"}, {2500000000000, "0.04"}, {0, "0.05"}} {
		net, err := baseCreditAmount(q, "51021", big.NewInt(tc.fee), cap)
		if err != nil || net.StringFixed(2) != tc.net {
			t.Fatal(tc, net, err)
		}
	}
	q.NetUSD = "0.02"
	if _, err := baseCreditAmount(q, "51021", big.NewInt(10), cap); err == nil {
		t.Fatal("inconsistent original quote")
	}
}
