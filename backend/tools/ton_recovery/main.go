// Explicit local operations: inspect/retry an original intent or run one recovery
// step. No daemon, signing capability, new payment, migration or API is started.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/svc"
)

func run() (err error) {
	path := flag.String("f", "api/etc/platform.yaml", "application config with explicit recovery policy")
	network := flag.String("network", chainassets.TONTestnet, "ton-testnet, solana-devnet or base-sepolia")
	action := flag.String("action", "list", "list, retry or run-once")
	state := flag.String("state", "", "recovery state; empty for all signed intents")
	limit := flag.Int("limit", 20, "list limit 1..1000")
	key := flag.String("key", "", "original request key to resume")
	reason := flag.String("reason", "", "required explanation for retry")
	flag.Parse()
	if *action != "list" && *action != "retry" && *action != "run-once" {
		return fmt.Errorf("invalid recovery action")
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	ton, sol, base := c.TONRecovery, c.SolanaRecovery, c.BaseRecovery
	timeout := c.ShutdownTimeout
	switch *network {
	case chainassets.TONTestnet:
		if ton == nil {
			return fmt.Errorf("TONRecovery configuration required")
		}
		timeout = ton.OperationTimeout
	case chainassets.BaseSepolia:
		if base == nil {
			return fmt.Errorf("BaseRecovery configuration required")
		}
		timeout = base.OperationTimeout
	case chainassets.SolanaDevnet:
		if sol == nil {
			return fmt.Errorf("SolanaRecovery configuration required")
		}
		timeout = sol.OperationTimeout
	default:
		return fmt.Errorf("unsupported recovery network %q", *network)
	}
	c.Background, c.TONRecovery, c.SolanaRecovery, c.BaseRecovery = nil, nil, nil, nil
	c.RocketMQ.Enabled, c.RocketMQ.Required = false, false
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	if *network == chainassets.BaseSepolia {
		recovery, err := chainsend.NewBaseRecovery(s.DB, s.ChainAssets, s.BaseReceipts, *base)
		if err != nil {
			return err
		}
		return runAction(ctx, recovery, *action, *state, *limit, *key, *reason)
	}
	if *network == chainassets.SolanaDevnet {
		recovery, err := chainsend.NewSolanaRecovery(s.DB, s.ChainAssets, s.ChainReceipts, *sol)
		if err != nil {
			return err
		}
		return runAction(ctx, recovery, *action, *state, *limit, *key, *reason)
	}
	w, err := ton.Wallet()
	if err != nil {
		return err
	}
	executor, err := chainsend.NewTON(s.TONSendIntents, w, nil, s.ChainAssets)
	if err != nil {
		return err
	}
	recovery, err := chainsend.NewRecovery(s.DB, executor, s.TONReceipts, *ton)
	if err != nil {
		return err
	}
	return runAction(ctx, recovery, *action, *state, *limit, *key, *reason)
}
func runAction[T any](ctx context.Context, recovery interface {
	List(context.Context, string, int) ([]T, error)
	Retry(context.Context, string, string, string) error
	RunOne(context.Context) (bool, error)
}, action, state string, limit int, key, reason string) error {
	if action == "retry" {
		operator, err := user.Current()
		if err != nil {
			return err
		}
		if err = recovery.Retry(ctx, key, operator.Username, reason); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"key": key, "state": chainsend.RecoveryReady, "operator": operator.Username, "reason": reason})
	}
	if action == "run-once" {
		work, stepErr := recovery.RunOne(ctx)
		rows, listErr := recovery.List(ctx, state, limit)
		outputErr := json.NewEncoder(os.Stdout).Encode(struct {
			Worked  bool `json:"worked"`
			Records []T  `json:"records"`
		}{work, rows})
		return errors.Join(stepErr, listErr, outputErr)
	}
	rows, err := recovery.List(ctx, state, limit)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(rows)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
