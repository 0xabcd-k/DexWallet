// chain_receive manages internal testnet collection requests and matches stored evidence.
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
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	var t chainreceipt.ReceiveTerms
	var event chainreceipt.EventKey
	action := flag.String("action", "get", "register, open, get or match")
	flag.StringVar(&t.Network, "network", "", "test network")
	flag.StringVar(&t.Key, "key", "", "stable server-side payment attempt key")
	flag.StringVar(&t.AssetID, "asset", "", "exact asset ID")
	flag.StringVar(&t.Recipient, "recipient", "", "platform receiving owner (register requires it; omit on open to allocate a single-use address)")
	flag.StringVar(&t.Payer, "payer", "", "expected payer wallet owner")
	flag.StringVar(&t.Atoms, "atoms", "", "exact integer quantity")
	flag.StringVar(&t.Purpose, "purpose", "", "deposit or merchant_payment")
	flag.Int64Var(&t.BeneficiaryID, "beneficiary", 0, "internal user or merchant ID according to purpose")
	flag.Int64Var(&t.ExpiresAt, "expires", 0, "exclusive end of collection window, Unix seconds")
	route := flag.String("route", "", "single_use or ton_memo (register only)")
	flag.StringVar(&event.Signature, "tx", "", "verified receiving transaction ID")
	flag.StringVar(&event.Kind, "kind", "", "recorded transfer event kind")
	flag.IntVar(&event.Index, "index", -1, "original transfer event index")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch *action {
	case "register", "open", "get", "match":
	default:
		return fmt.Errorf("invalid action")
	}
	if *action == "match" && t.Key != "" {
		return fmt.Errorf("a match accepts an event location only")
	}
	dsn, err := mysql.ParseDSN(os.Getenv("DEXWALLET_MYSQL_DSN"))
	if err != nil || dsn.DBName == "" {
		return fmt.Errorf("dedicated DEXWALLET_MYSQL_DSN required")
	}
	dsn.ParseTime = true
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version int
	var dirty bool
	if err = db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&version, &dirty); err != nil {
		return err
	}
	if dirty || version < 32 {
		return fmt.Errorf("clean business schema >=32 required")
	}
	cfg := chainassets.Config{}
	cfg.LoadEnvironment()
	assets, err := chainassets.New(cfg, nil)
	if err != nil {
		return err
	}
	defer assets.Close()
	service := chainreceipt.NewAttribution(sqlx.NewSqlConnFromDB(db), assets.Catalog())
	var out chainreceipt.ReceiveRequest
	switch *action {
	case "register":
		return service.Register(ctx, t.Network, t.Recipient, *route)
	case "open":
		out, err = service.Open(ctx, t)
	case "get":
		out, err = service.Find(ctx, t.Key)
	case "match":
		event.Network = t.Network
		out, err = service.Match(ctx, event)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func main() {
	logx.Disable()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
