// chain_receipts verifies one exact transfer event. It never credits an order.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	var request chainreceipt.Request
	flag.StringVar(&request.Network, "network", "", "configured Solana network")
	flag.StringVar(&request.Signature, "signature", "", "transaction's first signature")
	flag.IntVar(&request.Instruction, "instruction", -1, "top-level instruction index, zero-based")
	flag.StringVar(&request.AssetID, "asset", "", "exact catalog asset ID")
	flag.StringVar(&request.Recipient, "recipient", "", "expected receiving wallet owner (not SPL token account)")
	flag.StringVar(&request.Atoms, "atoms", "", "positive integer quantity in the asset's smallest unit")
	record := flag.Bool("record", false, "persist verified facts using DEXWALLET_MYSQL_DSN; does not credit any balance")
	timeout := flag.Duration("timeout", 10*time.Second, "total RPC query timeout, maximum 30s")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	config := chainassets.Config{Timeout: *timeout}
	config.LoadEnvironment()
	source, err := chainassets.New(config, nil)
	if err != nil {
		return err
	}
	defer source.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+5*time.Second)
	defer cancel()
	var receipt chainreceipt.Receipt
	reused := false
	if !*record {
		receipt, err = chainreceipt.NewVerifier(source).Verify(ctx, request)
	} else {
		dsn := os.Getenv("DEXWALLET_MYSQL_DSN")
		if dsn == "" {
			return fmt.Errorf("record requires DEXWALLET_MYSQL_DSN")
		}
		db, e := sql.Open("mysql", dsn)
		if e != nil {
			return e
		}
		defer db.Close()
		var version int
		var dirty bool
		if e = db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&version, &dirty); e != nil {
			return e
		}
		if version < 16 || dirty {
			return fmt.Errorf("record requires clean business schema >=16")
		}
		saved, e := chainreceipt.New(sqlx.NewSqlConnFromDB(db), transaction.New(db), source).Record(ctx, request)
		receipt, reused, err = saved.Receipt, saved.Reused, e
	}
	if err != nil {
		return err
	}
	out := struct {
		Receipt  chainreceipt.Receipt `json:"receipt"`
		Recorded bool                 `json:"recorded"`
		Reused   bool                 `json:"reused"`
	}{receipt, *record, reused}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
