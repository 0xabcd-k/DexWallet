// tron_deposit_nile executes or recovers an already authorized test deposit by quote ID.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	idx := flag.String("quote", "", "original Nile deposit quote ID")
	rpc := flag.String("rpc", "https://nile.trongrid.io", "trusted Nile FullNode and Solidity API")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var v int
	if err = db.QueryRowContext(ctx, "SELECT version FROM schema_migrations_business WHERE dirty=0").Scan(&v); err != nil {
		return err
	}
	if v < 46 {
		return errors.New("Nile deposits require clean business schema >=46")
	}
	node, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TRONNile, URL: *rpc, TestUSDTContract: chainassets.NileDepositUSDTContract}}}, nil)
	if err != nil {
		return err
	}
	defer node.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	exec := deposit.NewTronExecution(conn, transaction.New(db), account.New(accounts.NewUserAccountModel(conn)), node)
	for {
		o, err := exec.Step(ctx, *idx)
		if o != nil {
			fmt.Printf("order=%s state=%s tx=%s", o.QuoteIdx, o.State, o.TxId)
			if o.ActualFeeSun.Valid {
				fmt.Printf(" asset=%s fee_sun=%d bandwidth=%d energy=%d", o.AssetId, o.ActualFeeSun.Int64, o.ActualBandwidth.Int64, o.ActualEnergy.Int64)
			}
			if o.CreditedUsd.Valid {
				fmt.Printf(" credited_usd=%s", o.CreditedUsd.Decimal.StringFixed(2))
			}
			fmt.Println()
		}
		if err != nil {
			return err
		}
		if o.State == deposit.TronCredited {
			return nil
		}
		if o.State == deposit.TronReview {
			return errors.New("Nile deposit requires review")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
