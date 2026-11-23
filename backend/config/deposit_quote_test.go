package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestTestnetDepositQuoteConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	raw, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte(`
ChainAssets:
  Networks:
    - Network: solana-devnet
      URL: "http://127.0.0.1:1"
      TestUSDTContract: 2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5
  DepositQuotes:
    Version: example-1
    AsOf: 100
    ValidUntil: 200
    SolanaSOL:
      FeePayer: DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN
      Recipient: 7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5
    SolanaUSDC:
      FeePayer: DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN
      Recipient: 7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5
    SolanaTestUSDT:
      FeePayer: DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN
      Recipient: 7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5
    Assets:
      - AssetID: solana-devnet:usdt:2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5
        USDPerCoin: "0.98"
        EstimatedGas: "0"
      - AssetID: solana-devnet:usdc
        USDPerCoin: "1"
        EstimatedGas: "0"
      - AssetID: solana-devnet:sol
        USDPerCoin: "100"
        EstimatedGas: "0.00001"
      - AssetID: base-sepolia:eth
        USDPerCoin: "2000"
        EstimatedGas: "0.0001"
`)...)
	path := filepath.Join(t.TempDir(), "quote.yaml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := chainassets.New(cfg.ChainAssets, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for _, asset := range []string{":sol", ":usdc", ":usdt:2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"} {
		quote, err := source.PreviewDeposit(chainassets.SolanaDevnet+asset, "0.01", time.Unix(120, 0))
		if err != nil || !quote.WalletFeePreview {
			t.Fatal(quote, err)
		}
	}
	for _, env := range [][2]string{{"prod", "dev"}, {"local", "pro"}} {
		cfg.Environment, cfg.Mode = env[0], env[1]
		if err := cfg.Check(); err == nil {
			t.Fatal("test prices accepted", env)
		}
	}
}
