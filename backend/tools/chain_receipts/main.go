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
	flag.StringVar(&request.Network, "network", "", "configured Solana, Base, TRON or TON network")
	flag.StringVar(&request.Signature, "signature", "", "Solana signature, Base/TRON hash, or TON recipient transaction hash")
	logIndex := flag.Int("log-index", -1, "Base receipt logIndex or TRON info.log array index; omit for native transfers")
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
	isBase := request.Network == chainassets.BaseMainnet || request.Network == chainassets.BaseSepolia
	isTON := request.Network == chainassets.TONMainnet || request.Network == chainassets.TONTestnet
	tonRequest := chainreceipt.TONRequest{Network: request.Network, Hash: request.Signature, AssetID: request.AssetID, Recipient: request.Recipient, Atoms: request.Atoms}
	isTRON := request.Network == chainassets.TRONMainnet || request.Network == chainassets.TRONNile
	tronRequest := chainreceipt.TRONRequest{Network: request.Network, Hash: request.Signature, AssetID: request.AssetID, Recipient: request.Recipient, Atoms: request.Atoms}
	baseRequest := chainreceipt.BaseRequest{Network: request.Network, Hash: request.Signature, AssetID: request.AssetID, Recipient: request.Recipient, Atoms: request.Atoms}
	if *logIndex >= 0 {
		baseRequest.LogIndex = logIndex
		tronRequest.LogIndex = logIndex
	}
	if *logIndex < -1 || ((isBase || isTRON || isTON) && request.Instruction != -1) || (!isBase && !isTRON && *logIndex != -1) {
		return fmt.Errorf("event position does not match network")
	}
	var receipt chainreceipt.Receipt
	reused := false
	if !*record {
		if isTON {
			receipt, err = chainreceipt.NewTONVerifier(source).Verify(ctx, tonRequest)
		} else if isTRON {
			receipt, err = chainreceipt.NewTRONVerifier(source).Verify(ctx, tronRequest)
		} else if isBase {
			receipt, err = chainreceipt.NewBaseVerifier(source).Verify(ctx, baseRequest)
		} else {
			receipt, err = chainreceipt.NewVerifier(source).Verify(ctx, request)
		}
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
		if version < 18 || dirty {
			return fmt.Errorf("record requires clean business schema >=18")
		}
		var saved chainreceipt.Stored
		conn, tx := sqlx.NewSqlConnFromDB(db), transaction.New(db)
		if isTON {
			saved, e = chainreceipt.NewTON(conn, tx, source).Record(ctx, tonRequest)
		} else if isTRON {
			saved, e = chainreceipt.NewTRON(conn, tx, source).Record(ctx, tronRequest)
		} else if isBase {
			saved, e = chainreceipt.NewBase(conn, tx, source).Record(ctx, baseRequest)
		} else {
			saved, e = chainreceipt.New(conn, tx, source).Record(ctx, request)
		}
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
