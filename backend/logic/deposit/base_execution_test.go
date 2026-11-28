package deposit

import (
	"math/big"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestBaseUSDCCreditUsesActualNetworkFee(t *testing.T) {
	q := chainassets.DepositPreview{AssetID: baseusdc.AssetID, USDPerCoin: "0.98", GasUSDPerCoin: "4000", NetUSD: "0.01"}
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

func TestNativeDepositCreditUsesWeiAndOneRate(t *testing.T) {
	q := chainassets.DepositPreview{AssetID: chainassets.BaseSepoliaETH, USDPerCoin: "2000", GasUSDPerCoin: "2000", NetUSD: "0.01"}
	// Gross 0.000015 ETH, fee budget 0.00001 ETH, actual fee 0.000001 ETH.
	net, err := baseCreditAmount(q, "15000000000000", big.NewInt(1000000000000), big.NewInt(10000000000000))
	if err != nil || net.StringFixed(2) != "0.02" {
		t.Fatal(net, err)
	}
	q.GasUSDPerCoin = "1999"
	if _, err = baseCreditAmount(q, "15000000000000", big.NewInt(1000000000000), big.NewInt(10000000000000)); err == nil {
		t.Fatal("mismatched rates")
	}
}
