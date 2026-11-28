// ton_deposit_testnet executes or recovers an already authorized test deposit by quote ID.
package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/deposit"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type operator struct {
	*chainassets.Service
	route chainassets.TONDepositFeeConfig
}

func (n operator) CheckTONDeposit(ctx context.Context, q chainassets.DepositPreview, u, p []byte) error {
	return n.RecheckTONRelay(ctx, q, u, p, n.route)
}

func run() error {
	payer := flag.String("payer", "", "expected source W5 address (raw)")
	platform := flag.String("platform", "", "expected platform W5 address (raw), required to broadcast")
	emulator := flag.String("tonapi", "https://testnet.tonapi.io", "TonAPI testnet for pre-broadcast emulation with placeholder signatures")
	keyFile := flag.String("platform-phrase-file", "", "dedicated test W5 mnemonic file (first signature only)")
	gross := flag.Int64("max-gross-nano", 0, "maximum user transfer in nanoTON")
	jetton := flag.Int64("max-jetton-atoms", 0, "maximum test USDT in six-decimal units (Jetton orders only)")
	fee := flag.Int64("max-fee-nano", 0, "maximum authorized network fee in nanoTON")
	idx := flag.String("quote", "", "original TON Testnet deposit quote ID")
	rpc := flag.String("rpc", "https://testnet.toncenter.com", "trusted TON Center testnet")
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
	if v < 48 {
		return errors.New("TON deposits require clean business schema >=48")
	}
	node, err := chainassets.New(chainassets.Config{Timeout: 20 * time.Second, Networks: []chainassets.Endpoint{{Network: chainassets.TONTestnet, TestUSDTContract: chainassets.TONDepositTestMaster, URL: *rpc}}}, nil)
	if err != nil {
		return err
	}
	defer node.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	var key ed25519.PrivateKey
	if *keyFile != "" {
		raw, e := os.ReadFile(*keyFile)
		if e != nil {
			return e
		}
		defer clear(raw)
		key, e = wallet.SeedToPrivateKey(strings.Fields(string(raw)), "", false)
		if e != nil {
			return e
		}
		defer clear(key)
	}
	b := deposit.TonBudget{Sender: *payer, Recipient: *platform, MaxGrossNano: *gross, MaxJettonAtoms: *jetton, MaxFeeNano: *fee}
	exec := deposit.NewTonExecution(conn, transaction.New(db), account.New(accounts.NewUserAccountModel(conn)), operator{node, chainassets.TONDepositFeeConfig{PlatformWallet: *platform, TonAPIURL: *emulator}})
	for {
		o, err := exec.Step(ctx, *idx, b, key)
		if o != nil {
			fmt.Printf("order=%s state=%s dispatch=%s receive=%s", o.QuoteIdx, o.State, o.DispatchHash.String, o.ReceiveHash.String)
			if o.ActualFeeNano.Valid {
				fmt.Printf(" fee_nano=%d", o.ActualFeeNano.Int64)
			}
			if o.CreditedUsd.Valid {
				fmt.Printf(" credited_usd=%s", o.CreditedUsd.Decimal.StringFixed(2))
			}
			fmt.Println()
		}
		if err != nil {
			return err
		}
		if o.State == deposit.TonCredited {
			return nil
		}
		if o.State == deposit.TonReview {
			return errors.New("TON deposit requires review")
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
