// drawdemo publishes the candidate prize version only to an explicitly isolated test database.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func run() error {
	database := flag.String("database", "", "explicit isolated test database")
	flag.Parse()
	cfg, err := testenv.ValidateDSN(os.Getenv("DEXWALLET_MYSQL_DSN"), "migration")
	if err != nil {
		return err
	}
	if *database == "" || cfg.DBName != *database || flag.NArg() != 0 {
		return fmt.Errorf("explicit --database must match the isolated DSN")
	}
	codec, err := idcodec.New(os.Getenv("DEXWALLET_ID_SECRET"), idcodec.DrawVersion)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := sqlx.NewSqlConnFromDB(db)
	s := draw.New(conn, transaction.New(db), account.New(accounts.NewUserAccountModel(conn)), true)
	v, err := s.Publish(ctx, "candidate_v1", draw.SampleRules())
	if err != nil {
		return err
	}
	idx, err := codec.Encode(v.Id)
	if err != nil {
		return err
	}
	fmt.Printf("Published isolated candidate %s (%s); local/dev page /#/draws. No user points or cash were created.\n", v.Name, idx)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
