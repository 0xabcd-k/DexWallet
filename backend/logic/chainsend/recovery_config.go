package chainsend

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
)

// Recovery has no signing credentials. It can only inspect or replay an existing
// testnet authorization; creating and signing a new intent remains explicit.
type RecoveryConfig struct {
	RecoveryPolicy
	PublicKey    string
	SubwalletID  uint32
	JettonMaster string `json:",optional"`
}

type SolanaRecoveryConfig struct {
	RecoveryPolicy
	Sender string
}

type TRONRecoveryConfig struct {
	RecoveryPolicy
	Sender string
}

func (c TRONRecoveryConfig) Check() error {
	if _, err := tronnative.AddressBytes(c.Sender); err != nil {
		return fmt.Errorf("TRON recovery requires a Nile sender address: %w", err)
	}
	return c.RecoveryPolicy.Check()
}

type BaseRecoveryConfig struct {
	RecoveryPolicy
	Sender string
}

func (c BaseRecoveryConfig) Check() error {
	if _, err := baseAddress(c.Sender); err != nil {
		return fmt.Errorf("Base recovery requires a Sepolia sender address: %w", err)
	}
	return c.RecoveryPolicy.Check()
}

type RecoveryPolicy struct {
	Workers          int           `json:",default=1"`
	PollInterval     time.Duration `json:",default=5s"`
	OperationTimeout time.Duration `json:",default=30s"`
	LeaseDuration    time.Duration `json:",default=60s"`
	RetryInterval    time.Duration `json:",default=30s"`
	MaxAttempts      int64         `json:",default=60"`
}

func RecoveryDefaults() RecoveryConfig {
	return RecoveryConfig{RecoveryPolicy: DefaultRecoveryPolicy()}
}

func DefaultRecoveryPolicy() RecoveryPolicy {
	return RecoveryPolicy{Workers: 1, PollInterval: 5 * time.Second, OperationTimeout: 30 * time.Second, LeaseDuration: time.Minute, RetryInterval: 30 * time.Second, MaxAttempts: 60}
}
func (c RecoveryConfig) Wallet() (Wallet, error) {
	pub, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return Wallet{}, fmt.Errorf("TON recovery requires a testnet public key in base64")
	}
	master := c.JettonMaster
	if master != "" {
		master, err = rawTONAddress(master)
		if err != nil {
			return Wallet{}, err
		}
	}
	return Wallet{PublicKey: pub, SubwalletID: c.SubwalletID, JettonMaster: master}, nil
}
func (c RecoveryConfig) Check() error {
	if _, err := c.Wallet(); err != nil {
		return err
	}
	return c.RecoveryPolicy.Check()
}
func (c SolanaRecoveryConfig) Check() error {
	if _, err := solanaKey(c.Sender); err != nil {
		return fmt.Errorf("Solana recovery requires a Devnet sender public key: %w", err)
	}
	return c.RecoveryPolicy.Check()
}
func (c RecoveryPolicy) Check() error {
	if c.Workers < 1 || c.Workers > 8 || c.PollInterval <= 0 || c.OperationTimeout <= 0 || c.LeaseDuration <= c.OperationTimeout || c.RetryInterval <= 0 || c.RetryInterval > 24*time.Hour || c.MaxAttempts < 1 || c.MaxAttempts > 10000 {
		return fmt.Errorf("invalid recovery concurrency, timeout, lease or retry policy")
	}
	return nil
}
