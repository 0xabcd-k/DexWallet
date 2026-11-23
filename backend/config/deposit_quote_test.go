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
    - Network: ton-testnet
      URL: "http://127.0.0.1:1"
    - Network: tron-nile
      TestUSDTContract: TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn
      URL: "http://127.0.0.1:1"
    - Network: base-sepolia
      URL: "http://127.0.0.1:1"
    - Network: solana-devnet
      URL: "http://127.0.0.1:1"
      TestUSDTContract: 2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5
  DepositQuotes:
    Version: example-1
    AsOf: 100
    ValidUntil: 200
    TONNative:
      PlatformWallet: "0:100769e6dae579cfbc5ca1637c88d6c83a6c2195b3e571f1e35f20b45b2fa4d0"
      TonAPIURL: "https://testnet.tonapi.io"
    TRONTestUSDT:
      ResourceOwner: TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH
      Recipient: TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH
    TRONTRX:
      ResourceOwner: TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH
      Recipient: TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH
    BaseETH:
      FeePayer: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
      Recipient: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
    BaseUSDC:
      FeePayer: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
      Recipient: "0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"
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
      - AssetID: ton-testnet:ton
        USDPerCoin: "1"
        EstimatedGas: "0.001"
      - AssetID: solana-devnet:usdt:2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5
        USDPerCoin: "0.98"
        EstimatedGas: "0"
      - AssetID: solana-devnet:usdc
        USDPerCoin: "1"
        EstimatedGas: "0"
      - AssetID: solana-devnet:sol
        USDPerCoin: "100"
        EstimatedGas: "0.00001"
      - AssetID: base-sepolia:usdc
        USDPerCoin: "0.98"
        EstimatedGas: "0.00001"
      - AssetID: tron-nile:usdt:TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn
        USDPerCoin: "1"
        EstimatedGas: "0.1"
      - AssetID: tron-nile:trx
        USDPerCoin: "0.30"
        EstimatedGas: "0.1"
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
	for _, asset := range []string{"ton-testnet:ton", chainassets.NileUSDT("TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn").ID, chainassets.TRONNileTRX, chainassets.BaseSepoliaETH, "base-sepolia:usdc", "solana-devnet:sol", "solana-devnet:usdc", "solana-devnet:usdt:2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5"} {
		quote, err := source.PreviewDeposit(asset, "0.01", time.Unix(120, 0))
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
