package deposit

import (
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/shopspring/decimal"
)

func TestJettonProceedsDoNotChargeExistingSenderFundsTwice(t *testing.T) {
	// Use the observed amounts with prices placing the deduction across a cent.
	q := chainassets.DepositPreview{AssetID: chainassets.TONDepositTestUSDT, PayDecimals: 6, USDPerCoin: "2", GasUSDPerCoin: "7", NetUSD: "0.01"}
	fee := decimal.New(3643213, -9)
	got, err := tonDepositProceeds(q, "17640", fee, 75086)
	if err != nil || got.StringFixed(2) != "0.01" {
		t.Fatal("sender-paid rent reduced the credit", got, err)
	}
	if _, err = tonDepositProceeds(q, "17640", fee, 0); err == nil {
		t.Fatal("insufficient net proceeds accepted")
	}
}
