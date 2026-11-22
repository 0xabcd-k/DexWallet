package chainassets

import (
	"testing"
	"time"
)

func TestDepositPreview(t *testing.T) {
	cfg := DepositQuoteConfig{Version: "fixture-1", AsOf: 100, ValidUntil: 200, Assets: []DepositQuoteRate{
		{BaseSepoliaETH, "2000", "0.00001"}, {BaseSepolia + ":usdc", "0.98", "0.0001"},
		{SolanaDevnet + ":sol", "3", "0"},
	}}
	service, err := New(Config{DepositQuotes: &cfg}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	q, err := service.PreviewDeposit(BaseSepolia+":usdc", "10", time.Unix(120, 0))
	if err != nil || q.PayQuantity != "10.408164" || q.GasUSD != "0.2" || q.NetUSD != "10.00" || q.TargetUSD != "10.00" || q.GasAssetID != BaseSepoliaETH || q.ExpiresAt != 150000 || q.Version != cfg.Version {
		t.Fatal(q, err)
	}
	for _, tc := range []struct{ asset, target, want string }{
		{BaseSepoliaETH, "0.01", "0.000015"}, {SolanaDevnet + ":sol", "0.01", "0.003333334"},
	} {
		q, err := service.PreviewDeposit(tc.asset, tc.target, time.Unix(199, 0))
		if err != nil || q.PayQuantity != tc.want || q.NetUSD != tc.target || q.ExpiresAt != 200000 {
			t.Fatal(q, err)
		}
	}
	for _, at := range []int64{99, 200, 201} {
		if _, err := service.PreviewDeposit(BaseSepoliaETH, "1", time.Unix(at, 0)); err == nil {
			t.Fatal(at)
		}
	}
	for _, input := range []string{"0", "1e2", "1.001", "0.010", "01", "-1", "1000000000000000000"} {
		if _, err := service.PreviewDeposit(BaseSepoliaETH, input, time.Unix(101, 0)); err == nil {
			t.Fatal(input)
		}
	}
	if _, err := service.PreviewDeposit(SolanaDevnet+":sol", "999999999999999999.99", time.Unix(110, 0)); err == nil {
		t.Fatal("accepted quantity outside u64")
	}
	if service.HasDepositPreview(BaseMainnet+":eth", time.Unix(110, 0)) || service.HasDepositPreview(TRONNile+":trx", time.Unix(110, 0)) {
		t.Fatal("unconfigured preview")
	}
}

func TestDepositSheetValidation(t *testing.T) {
	valid := DepositQuoteRate{BaseSepoliaETH, "2000", "0.000003"}
	for _, rows := range [][]DepositQuoteRate{nil, {valid, valid}, {{BaseMainnet + ":eth", "1", "0"}}, {{TONTestnet + ":usdt:other", "1", "0"}}, {{BaseSepolia + ":usdc", "1", "0"}},
		{{BaseSepoliaETH, "0", "0"}}, {{BaseSepoliaETH, "1e4", "0"}}, {{BaseSepoliaETH, "2000", ""}}, {{BaseSepoliaETH, "2000", "0.0000000000000000001"}}} {
		if s, err := New(Config{DepositQuotes: &DepositQuoteConfig{Version: "fixture", AsOf: 1, ValidUntil: 20, Assets: rows}}, nil); err == nil {
			s.Close()
			t.Fatal(rows)
		}
	}
}

func TestDepositQuotesUseExactTestAssetIDs(t *testing.T) {
	const node = "http://127.0.0.1:1"
	endpoints := []Endpoint{{Network: TONTestnet, URL: node, TestUSDTContract: tonOwner}, {Network: SolanaDevnet, URL: node, TestUSDTContract: solOwner}, {Network: TRONNile, URL: node, TestUSDTContract: tronOwner}}
	assets := []string{TONTestnet + ":ton", TONTestnet + ":usdt:" + tonOwner, BaseSepoliaETH, BaseSepolia + ":usdc", SolanaDevnet + ":sol", SolanaDevnet + ":usdc", SolanaDevnet + ":usdt:" + solOwner, TRONNile + ":trx", TRONNile + ":usdt:" + tronOwner}
	cfg := &DepositQuoteConfig{Version: "four-networks", AsOf: 100, ValidUntil: 200}
	for _, id := range assets {
		cfg.Assets = append(cfg.Assets, DepositQuoteRate{id, "2", "0.000001"})
	}
	s, err := New(Config{Networks: endpoints, DepositQuotes: cfg}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range assets {
		q, err := s.PreviewDeposit(id, "1", time.Unix(110, 0))
		if err != nil || q.AssetID != id || q.NetUSD != "1.00" || q.PayQuantity != "0.500001" {
			t.Fatal(id, q, err)
		}
	}
	if _, err := s.PreviewDeposit(TRONNile+":usdt", "1", time.Unix(110, 0)); err == nil {
		t.Fatal("accepted unspecified test token")
	}
}
