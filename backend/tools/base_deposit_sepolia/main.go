// base_deposit_sepolia executes or recovers one accepted local Base Sepolia deposit order.
package main

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type depositRPC struct {
	*chainassets.Service
	history *chainassets.Service
}

func (r depositRPC) ReadBaseDepositETHEvidence(ctx context.Context, network, hash string) (chainassets.BaseEvidence, error) {
	return r.history.ReadBaseDepositETHEvidence(ctx, network, hash)
}
func run() error {
	idx := flag.String("quote", "", "accepted Base Sepolia deposit quote ID")
	recipient := flag.String("recipient", "", "authorized Base collection address")
	gross := flag.String("usdc-atoms", "", "exact gross USDC millionths")
	eth := flag.String("eth-wei", "", "exact gross native ETH amount in wei")
	fee := flag.String("max-fee-wei", "", "maximum total sponsor expenditure in wei")
	keyFile := flag.String("sponsor-key-file", "", "dedicated Base Sepolia hex private key file (omit to recover original wire)")
	rpc := flag.String("rpc", "https://sepolia.base.org", "trusted Base Sepolia RPC")
	traceRPC := flag.String("trace-rpc", "https://base-testnet.api.pocket.network", "trusted Base Sepolia canonical receipts and full callTracer endpoint for native ETH")
	flag.Parse()
	if len(*idx) != 32 {
		return errors.New("original quote ID required")
	}
	dsn, err := testenv.ValidateDSN(os.Getenv("DEXWALLET_MYSQL_DSN"), "deposit-credit")
	if err != nil {
		return err
	}
	dsn.ParseTime = true
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 40*time.Minute)
	defer cancel()
	var version int
	if err = db.QueryRowContext(ctx, "SELECT version FROM schema_migrations_business WHERE dirty=0").Scan(&version); err != nil {
		return err
	}
	if version < 44 {
		return errors.New("clean business schema >=44 required")
	}
	var key *ecdsa.PrivateKey
	budget := deposit.BaseBudget{Recipient: *recipient}
	if *keyFile != "" {
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		defer clear(raw)
		key, err = crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x"))
		if err != nil {
			return errors.New("invalid dedicated test key")
		}
		if (*gross == "") == (*eth == "") {
			return errors.New("provide exactly one asset quantity")
		}
		budget.ETHWei, _ = new(big.Int).SetString(*eth, 10)
		budget.USDCAtoms, _ = new(big.Int).SetString(*gross, 10)
		budget.MaxFeeWei, _ = new(big.Int).SetString(*fee, 10)
	}
	node, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: *rpc}}}, nil)
	if err != nil {
		return err
	}
	defer node.Close()
	history, err := chainassets.New(chainassets.Config{Timeout: 25 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.BaseSepolia, URL: *traceRPC}}}, nil)
	if err != nil {
		return err
	}
	defer history.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	w := deposit.NewBaseExecution(conn, transaction.New(db), account.New(accounts.NewUserAccountModel(conn)), depositRPC{node, history})
	for {
		row, err := w.Step(ctx, *idx, budget, key)
		if row != nil {
			fmt.Printf("order=%s state=%s tx=%s fee_wei=%s credited_usd=%s\n", row.QuoteIdx, row.State, row.TxHash.String, row.ActualFeeWei, row.CreditedUsd.Decimal.StringFixed(2))
		}
		if err != nil {
			if row == nil || row.State != deposit.BaseSigned {
				return err
			}
			fmt.Fprintln(os.Stderr, err)
		} else if row.State == deposit.BaseCredited {
			return nil
		} else if row.State == deposit.BaseReview {
			return fmt.Errorf("order awaits operator attention: %s", row.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(6 * time.Second):
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
