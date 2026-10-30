//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	accountlogic "github.com/0xabcd-k/DexWallet/backend/logic/account"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	driver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestAccountSchemaUpgradeAndConstraints(t *testing.T) {
	ctx := context.Background()
	name, dsn, upgraded, err := testRun.NewDatabase(ctx, "account_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(4); err != nil {
		t.Fatal(err)
	}
	mustExec(t, upgraded, "INSERT INTO user(id,nickname) VALUES(1,'existing')")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = upgraded.QueryRow("SELECT COUNT(*) FROM user_account").Scan(&count); err != nil || count != 0 {
		t.Fatalf("upgrade eagerly created accounts: %d %v", count, err)
	}
	fresh, _ := userDatabase(t, "account_fresh")
	_, _, full, err := testRun.NewDatabase(ctx, "account_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, source := range []string{"user/tables.sql", "user/constraints.sql", "account/tables.sql", "account/constraints.sql"} {
		ddl, err := os.ReadFile("../../manifest/sql/ddl/" + source)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	var table, want, got string
	if err = full.QueryRow("SHOW CREATE TABLE user_account").Scan(&table, &want); err != nil {
		t.Fatal(err)
	}
	for _, db := range []*sql.DB{fresh, upgraded} {
		if err = db.QueryRow("SHOW CREATE TABLE user_account").Scan(&table, &got); err != nil || got != want {
			t.Fatalf("DDL differs: %v\n%s\n%s", err, got, want)
		}
	}
	mustExec(t, upgraded, "INSERT INTO user_account(user_id,kind,balance,held_balance) VALUES(1,?,12.34,1.00),(1,?,200,0),(1,?,2.00,0)", model.KindUSD, model.KindTaskPoints, model.KindRewardUSD)
	for _, tc := range []struct {
		sql  string
		code uint16
	}{
		{"INSERT INTO user_account(user_id,kind) VALUES(1,'usd_balance')", 1062},
		{"INSERT INTO user_account(user_id,kind) VALUES(999,'usd_balance')", 1452},
		{"DELETE FROM user WHERE id=1", 1451},
		{"UPDATE user_account SET balance=-1 WHERE kind='usd_balance'", 3819},
		{"UPDATE user_account SET held_balance=-1 WHERE kind='usd_balance'", 3819},
		{"UPDATE user_account SET balance=1.001 WHERE kind='usd_balance'", 3819},
		{"UPDATE user_account SET balance=1.1 WHERE kind='task_points'", 3819},
		{"UPDATE user_account SET held_balance=1 WHERE kind='reward_usd'", 3819},
	} {
		_, err := upgraded.Exec(tc.sql)
		var mysqlErr *driver.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != tc.code {
			t.Fatalf("constraint %s: %v", tc.sql, err)
		}
	}
	// An isolated rollback drops only this new table; old user data survives.
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	var nickname string
	if err = upgraded.QueryRow("SELECT nickname FROM user WHERE id=1").Scan(&nickname); err != nil || nickname != "existing" {
		t.Fatal("rollback changed user", err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.QueryRow("SELECT COUNT(*) FROM user_account").Scan(&count); err != nil || count != 0 {
		t.Fatal("round trip created accounts", err)
	}
}

func TestAccountQueryLifecycleAndExactStorage(t *testing.T) {
	db, users := userDatabase(t, "account_query")
	ctx := context.Background()
	email, err := users.ResolveOrCreateVerifiedEmail(ctx, "account@example.com")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := users.ResolveOrCreateVerifiedTelegram(ctx, userlogic.VerifiedTelegram{TelegramUserID: 12345, FirstName: "account test"})
	if err != nil {
		t.Fatal(err)
	}
	store := model.NewUserAccountModel(sqlx.NewSqlConnFromDB(db))
	accounts := accountlogic.New(store)
	for _, owner := range []int64{email.User.Id, tg.User.Id} {
		got, err := accounts.List(ctx, owner)
		if err != nil || len(got) != 3 || got[0].Balance != "0.00" || got[2].Balance != "0" {
			t.Fatalf("initial account: %+v %v", got, err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM user_account").Scan(&count); err != nil || count != 0 {
		t.Fatal("login/read wrote accounts", err)
	}
	mustExec(t, db, "INSERT INTO user_account(user_id,kind,balance,held_balance) VALUES(?,?,?,?)", email.User.Id, model.KindUSD, "9007199254740993.01", "2.09")
	got, err := accounts.List(ctx, email.User.Id)
	if err != nil || got[0].TotalBalance != "9007199254740995.10" || got[1].Balance != "0.00" {
		t.Fatalf("partial precise summary: %+v %v", got, err)
	}
	other, err := accounts.List(ctx, tg.User.Id)
	if err != nil || other[0].Balance != "0.00" {
		t.Fatal("cross-user balance", other, err)
	}
	if _, err = users.ResolveOrCreateVerifiedEmail(ctx, "account@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err = accounts.List(ctx, email.User.Id)
	if err != nil || got[0].Balance != "9007199254740993.01" {
		t.Fatal("login reset balance", got, err)
	}
	rollback := errors.New("rollback fixture")
	err = transaction.New(db).TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		bound := store.WithSession(session)
		if _, err := bound.Insert(ctx, &model.UserAccount{UserId: email.User.Id, Kind: model.KindTaskPoints, Balance: decimal.NewFromInt(200)}); err != nil {
			return err
		}
		rows, err := bound.ListByUserID(ctx, email.User.Id)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			t.Fatalf("session cannot read its own write: %+v", rows)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	got, err = accounts.List(ctx, email.User.Id)
	if err != nil || got[2].Balance != "0" {
		t.Fatal("session escaped transaction", got, err)
	}
	// Future account kinds can use the column's precision; no float conversion.
	precise := decimal.RequireFromString("999999999999999999.123456789012345678")
	if _, err = store.Insert(ctx, &model.UserAccount{UserId: tg.User.Id, Kind: "test_future", Balance: precise}); err != nil {
		t.Fatal(err)
	}
	row, err := store.FindOneByUserIdKind(ctx, tg.User.Id, "test_future")
	if err != nil || !row.Balance.Equal(precise) {
		t.Fatal("decimal storage changed value", row, err)
	}
	if got, err = accounts.List(ctx, tg.User.Id); err == nil || got != nil {
		t.Fatal("unknown stored kind silently ignored", got, err)
	}
}
