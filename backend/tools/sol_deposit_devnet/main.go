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

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	idx := flag.String("quote", "", "single accepted Solana quote idx")
	recipient := flag.String("recipient", "", "expected Devnet collection address")
	gross := flag.Int64("gross-lamports", 0, "exact gross transfer authorized for this order")
	usdc := flag.Uint64("usdc-atoms", 0, "exact gross Devnet USDC in millionths, mutually exclusive with gross-lamports")
	testUSDT := flag.Uint64("test-usdt-atoms", 0, "exact gross Devnet test USDT in millionths")
	mint := flag.String("test-usdt-mint", "", "expected Devnet test USDT mint for this order")
	funding := flag.Int64("max-funding-lamports", 0, "separate sponsor authorization for quoted SPL account funding")
	fee := flag.Int64("fee-lamports", 0, "authorized test sponsor fee, including on execution failure")
	keyFile := flag.String("sponsor-key-file", "", "dedicated Devnet sponsor CLI key file")
	rpc := flag.String("rpc", "https://api.devnet.solana.com", "trusted Devnet RPC")
	settleOnly := flag.Bool("settle-only", false, "credit an already finalized local Solana deposit without keys or RPC")
	flag.Parse()
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
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var version int
	var dirty bool
	if err = db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&version, &dirty); err != nil {
		return err
	}
	if dirty || version < 42 {
		return errors.New("clean business schema >=42 required")
	}
	conn := sqlx.NewSqlConnFromDB(db)
	credit := func() error {
		row, err := deposit.CreditSol(ctx, transaction.New(db), account.New(accounts.NewUserAccountModel(conn)), *idx)
		if err != nil {
			return err
		}
		fmt.Printf("order=%s credited_usd=%s credited_at=%s\n", row.OrderIdx, row.CreditedUsd.Decimal.StringFixed(2), row.CreditedAt.Time.UTC().Format(time.RFC3339Nano))
		return nil
	}
	if *settleOnly {
		return credit()
	}
	terms := chainassets.SolDepositTerms{Recipient: *recipient, GrossLamports: *gross, FeeLamports: *fee, MaxFundingLamports: *funding}
	if *usdc > 0 {
		terms.TokenAssetID, terms.TokenAtoms = chainassets.SolanaDevnetUSDC, *usdc
	}
	if *testUSDT > 0 || *mint != "" {
		if *usdc != 0 || *testUSDT == 0 || *mint == "" {
			return errors.New("test USDT requires its mint, quantity and a single transfer asset")
		}
		terms.TokenAssetID, terms.TokenAtoms = chainassets.SolanaDevnetTestUSDTPrefix+*mint, *testUSDT
	}
	if !terms.Valid() {
		return errors.New("invalid Devnet asset, gross or sponsor budget")
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
	cfg := chainassets.Config{Timeout: 10 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.SolanaDevnet, URL: *rpc}}}
	assets, err := chainassets.New(cfg, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	work, err := deposit.NewSolExecution(conn, assets, *idx, terms, key)
	if err != nil {
		return err
	}
	for {
		row, err := work.Step(ctx)
		if row != nil {
			fmt.Printf("order=%s state=%s tx=%s fee_lamports=%d funding_lamports=%d slot=%d\n", row.OrderIdx, row.ExecutionState, row.TxSignature.String, row.ActualLamportsFee, row.ActualFundingLamports, row.FinalizedSlot)
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
			return credit()
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
