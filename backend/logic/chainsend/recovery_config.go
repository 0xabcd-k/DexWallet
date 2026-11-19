package chainsend

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"
)

// Recovery has no signing credentials. It can only inspect or replay an existing
// testnet authorization; creating and signing a new intent remains explicit.
type RecoveryConfig struct {
	PublicKey        string
	SubwalletID      uint32
	JettonMaster     string        `json:",optional"`
	Workers          int           `json:",default=1"`
	PollInterval     time.Duration `json:",default=5s"`
	OperationTimeout time.Duration `json:",default=30s"`
	LeaseDuration    time.Duration `json:",default=60s"`
	RetryInterval    time.Duration `json:",default=30s"`
	MaxAttempts      int64         `json:",default=60"`
}

func RecoveryDefaults() RecoveryConfig {
	return RecoveryConfig{Workers: 1, PollInterval: 5 * time.Second, OperationTimeout: 30 * time.Second, LeaseDuration: time.Minute, RetryInterval: 30 * time.Second, MaxAttempts: 60}
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
	if c.Workers < 1 || c.Workers > 8 || c.PollInterval <= 0 || c.OperationTimeout <= 0 || c.LeaseDuration <= c.OperationTimeout || c.RetryInterval <= 0 || c.RetryInterval > 24*time.Hour || c.MaxAttempts < 1 || c.MaxAttempts > 10000 {
		return fmt.Errorf("invalid TON recovery concurrency, timeout, lease or retry policy")
	}
	return nil
}
