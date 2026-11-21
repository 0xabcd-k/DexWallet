package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTONRecoveryExplicitConfigurationAndBudgets(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	t.Setenv("DEXWALLET_CHAIN_TON_TESTNET_RPC_URL", "http://127.0.0.1:1")
	raw, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recovery.yaml")
	public := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	raw = append(raw, []byte("\nTONRecovery:\n  PublicKey: "+public+"\n  SubwalletID: 698983191\n")...)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.TONRecovery == nil || c.TONRecovery.Workers != 1 || c.TONRecovery.OperationTimeout != 30*time.Second || c.TONRecovery.MaxAttempts != 60 {
		t.Fatal("recovery defaults not loaded")
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Environment = "prod" }, func(c *Config) { c.Mode = "pro" }, func(c *Config) { c.ChainAssets.Networks = nil },
		func(c *Config) { c.TONRecovery.PublicKey = "invalid" }, func(c *Config) { c.TONRecovery.Workers = 0 },
		func(c *Config) { c.TONRecovery.LeaseDuration = c.TONRecovery.OperationTimeout },
		func(c *Config) { c.TONRecovery.OperationTimeout = c.ShutdownTimeout + time.Second },
		func(c *Config) { c.TONRecovery.MaxAttempts = 0 }, func(c *Config) { c.TONRecovery.RetryInterval = 0 },
		func(c *Config) { c.TONRecovery.JettonMaster = "0:" + strings.Repeat("ab", 32) },
	} {
		bad := c
		policy := *c.TONRecovery
		bad.TONRecovery = &policy
		change(&bad)
		if err = bad.Check(); err == nil {
			t.Fatal("unsafe recovery config accepted")
		}
	}
	c.TONRecovery = nil
	if err = c.Check(); err != nil {
		t.Fatal("optional recovery changed existing config", err)
	}
}

func TestSolanaRecoveryConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	t.Setenv("DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL", "http://127.0.0.1:1")
	raw, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("\nSolanaRecovery:\n  Sender: DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN\n  Workers: 3\n")...)
	path := filepath.Join(t.TempDir(), "solana.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SolanaRecovery.Workers != 3 || c.SolanaRecovery.MaxAttempts != 60 || c.TONRecovery != nil {
		t.Fatal("unexpected recovery policy")
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Environment = "prod" }, func(c *Config) { c.Mode = "pro" },
		func(c *Config) { c.SolanaRecovery.Sender = "bad" }, func(c *Config) { c.ChainAssets.Networks = nil },
		func(c *Config) { c.ShutdownTimeout = time.Second }, func(c *Config) { c.SolanaRecovery.LeaseDuration = c.SolanaRecovery.OperationTimeout },
	} {
		bad := c
		policy := *c.SolanaRecovery
		bad.SolanaRecovery = &policy
		change(&bad)
		if err := bad.Check(); err == nil {
			t.Fatal("invalid recovery policy accepted")
		}
	}
}

func TestBaseRecoveryConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	t.Setenv("DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL", "http://127.0.0.1:1")
	raw, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("\nBaseRecovery:\n  Sender: \"0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA\"\n  Workers: 3\n")...)
	path := filepath.Join(t.TempDir(), "base.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseRecovery.Workers != 3 || c.BaseRecovery.MaxAttempts != 60 || c.TONRecovery != nil || c.SolanaRecovery != nil {
		t.Fatal("unexpected recovery policy")
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Environment = "prod" }, func(c *Config) { c.Mode = "pro" },
		func(c *Config) { c.BaseRecovery.Sender = "bad" }, func(c *Config) { c.BaseRecovery.Sender = "0x" + strings.Repeat("0", 40) }, func(c *Config) { c.ChainAssets.Networks = nil },
		func(c *Config) { c.ShutdownTimeout = time.Second }, func(c *Config) { c.BaseRecovery.LeaseDuration = c.BaseRecovery.OperationTimeout },
	} {
		bad := c
		policy := *c.BaseRecovery
		bad.BaseRecovery = &policy
		change(&bad)
		if err := bad.Check(); err == nil {
			t.Fatal("invalid recovery policy accepted")
		}
	}
}

func TestTRONRecoveryConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	t.Setenv("DEXWALLET_CHAIN_TRON_NILE_RPC_URL", "http://127.0.0.1:1")
	raw, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("\nTRONRecovery:\n  Sender: \"TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH\"\n  Workers: 3\n")...)
	path := filepath.Join(t.TempDir(), "tron.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.TRONRecovery.Workers != 3 || c.TRONRecovery.MaxAttempts != 60 || c.TONRecovery != nil || c.SolanaRecovery != nil {
		t.Fatal("unexpected recovery policy")
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Environment = "prod" }, func(c *Config) { c.Mode = "pro" },
		func(c *Config) { c.TRONRecovery.Sender = "bad" }, func(c *Config) { c.TRONRecovery.Sender = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb" }, func(c *Config) { c.ChainAssets.Networks = nil },
		func(c *Config) { c.ShutdownTimeout = time.Second }, func(c *Config) { c.TRONRecovery.LeaseDuration = c.TRONRecovery.OperationTimeout },
	} {
		bad := c
		policy := *c.TRONRecovery
		bad.TRONRecovery = &policy
		change(&bad)
		if err := bad.Check(); err == nil {
			t.Fatal("invalid recovery policy accepted")
		}
	}
}
