// sol_deposit_devnet executes one accepted self-deposit against a trusted Devnet RPC.
package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	idx := flag.String("quote", "", "single accepted SOL quote idx")
	recipient := flag.String("recipient", "", "expected Devnet collection address")
	gross := flag.Int64("gross-lamports", 0, "exact gross transfer authorized for this order")
	fee := flag.Int64("fee-lamports", 0, "authorized test sponsor fee, including on execution failure")
	keyFile := flag.String("sponsor-key-file", "", "dedicated Devnet sponsor CLI key file")
	rpc := flag.String("rpc", "https://api.devnet.solana.com", "trusted Devnet RPC")
	flag.Parse()
	if os.Getenv("DEXWALLET_TEST_ENV") != "local" {
		return errors.New("set DEXWALLET_TEST_ENV=local for the dedicated Devnet test database")
	}
	raw, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	defer clear(raw)
	var key ed25519.PrivateKey
	if json.Unmarshal(raw, &key) != nil {
		return errors.New("invalid Devnet CLI key file")
	}
	defer clear(key)
	dsn, err := mysql.ParseDSN(os.Getenv("DEXWALLET_MYSQL_DSN"))
	if err != nil || dsn.DBName == "" {
		return errors.New("dedicated DEXWALLET_MYSQL_DSN required")
	}
	dsn.ParseTime = true
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	cfg := chainassets.Config{Timeout: 10 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: *rpc}}}
	assets, err := chainassets.New(cfg, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	work, err := deposit.NewSolExecution(sqlx.NewSqlConnFromDB(db), assets, *idx, chainassets.SolDepositTerms{Recipient: *recipient, GrossLamports: *gross, FeeLamports: *fee}, key)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var version int
	var dirty bool
	if err = db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&version, &dirty); err != nil {
		return err
	}
	if dirty || version < 39 {
		return errors.New("clean business schema >=39 required")
	}
	for {
		row, err := work.Step(ctx)
		if row != nil {
			fmt.Printf("order=%s state=%s tx=%s fee_lamports=%d slot=%d\n", row.OrderIdx, row.ExecutionState, row.TxSignature.String, row.ActualLamportsFee, row.FinalizedSlot)
		}
		if err != nil {
			// An RPC failure after signing is ambiguous. Keep observing the original
			// ID on this run, and across invocations if the deadline is reached.
			if row == nil || (row.ExecutionState != deposit.SolSigned && row.ExecutionState != deposit.SolSubmitted) {
				return err
			}
			fmt.Fprintln(os.Stderr, err)
		}
		switch row.ExecutionState {
		case deposit.SolFinalized:
			return nil
		case deposit.SolFailed, deposit.SolReview:
			return fmt.Errorf("order requires inspection: %s", row.ExecutionState)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
