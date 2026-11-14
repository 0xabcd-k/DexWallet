// taskdemo adds one sample task to an explicitly selected isolated test database.
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
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
)

const sampleChannelID int64 = -100900000001
const sampleValidity = 24 * time.Hour

func run() error {
	database := flag.String("database", "", "explicit isolated test database")
	user := flag.Int64("telegram-user-id", 900000001, "Telegram identity from local development login")
	status := flag.String("member-status", tasks.MemberRegular, "simulated Telegram member status")
	flag.Parse()
	cfg, err := testenv.ValidateDSN(os.Getenv("DEXWALLET_MYSQL_DSN"), "migration")
	if err != nil {
		return err
	}
	if *database == "" || cfg.DBName != *database || flag.NArg() != 0 {
		return fmt.Errorf("explicit --database must match the isolated DSN")
	}
	if _, err = tasks.NewMock([]tasks.Fixture{{ChannelID: sampleChannelID, TelegramUserID: *user, Status: *status}}); err != nil {
		return err
	}
	codec, err := idcodec.New(os.Getenv("DEXWALLET_ID_SECRET"), idcodec.Task)
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
	result, err := db.ExecContext(ctx, "INSERT INTO task_definition(kind,status,verification_mode,title,description,base_points,channel_id,starts_at,ends_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL ? SECOND))", tasks.KindTelegramChannelFollow, tasks.StatusActive, tasks.ModeMock, "Telegram 频道关注样例 / Telegram channel sample", "无需关注真实频道 / No real subscription required", 100, sampleChannelID, int64(sampleValidity/time.Second))
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	idx, err := codec.Encode(id)
	if err != nil {
		return err
	}
	fmt.Printf("Created sample task %s; page /#/tasks/%s\nLocal development configuration (restart backend after editing):\nTasks:\n  Mode: mock\n  Fixtures:\n    - ChannelID: %d\n      TelegramUserID: %d\n      Status: %s\n", idx, idx, sampleChannelID, *user, *status)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
