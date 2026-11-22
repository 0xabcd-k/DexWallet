package config

import (
	"os"
	"path/filepath"
	"testing"

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
  DepositQuotes:
    Version: example-1
    AsOf: 100
    ValidUntil: 200
    Assets:
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
	source.Close()
	for _, env := range [][2]string{{"prod", "dev"}, {"local", "pro"}} {
		cfg.Environment, cfg.Mode = env[0], env[1]
		if err := cfg.Check(); err == nil {
			t.Fatal("test prices accepted", env)
		}
	}
}
