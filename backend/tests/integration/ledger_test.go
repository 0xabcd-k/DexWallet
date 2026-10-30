//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ledgerFixture struct {
	db       *sql.DB
	accounts *account.Service
	tx       *transaction.Runner
}

func newLedger(t *testing.T, label string) *ledgerFixture {
	t.Helper()
	db, _ := userDatabase(t, label)
	mustExec(t, db, "INSERT INTO user(id,nickname) VALUES(1,'one'),(2,'two'),(3,'three')")
	return &ledgerFixture{db, account.New(model.NewUserAccountModel(sqlx.NewSqlConnFromDB(db))), transaction.New(db)}
}
func (f *ledgerFixture) post(p account.Posting) (int64, error) {
	var id int64
	err := f.tx.TransactCtx(context.Background(), func(ctx context.Context, s sqlx.Session) error {
		var err error
		id, err = f.accounts.Post(ctx, s, p)
		return err
	})
	return id, err
}
func funding(id, user int64, amount string) account.Posting {
	d := decimal.RequireFromString(amount)
	return account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: id, Action: account.ActionPost}, Entries: []account.Entry{
		account.Credit(user, model.KindUSD, d), {System: account.SystemUSDClearing, Side: account.SideDebit, Amount: d},
	}}
}
func transfer(id, from, to int64, amount string) account.Posting {
	d := decimal.RequireFromString(amount)
	return account.Posting{Source: account.Source{Type: account.BusinessTransfer, ID: id, Action: account.ActionPost}, Entries: []account.Entry{account.Debit(from, model.KindUSD, d), account.Credit(to, model.KindUSD, d)}}
}
func ledgerBalance(t *testing.T, f *ledgerFixture, user int64, want string) {
	t.Helper()
	got, err := f.accounts.List(context.Background(), user)
	if err != nil || got[0].Balance != want {
		t.Fatalf("user %d balance %+v, want %s: %v", user, got, want, err)
	}
}
func ledgerCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func assertLedgerBalanced(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT voucher_id,unit FROM ledger_entry GROUP BY voucher_id,unit HAVING SUM(CASE side WHEN 'credit' THEN amount ELSE -amount END)<>0) imbalanced`).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("unbalanced vouchers %d: %v", n, err)
	}
	// Fixtures open at zero; every stored available balance must equal its entries.
	err = db.QueryRow(`SELECT COUNT(*) FROM user_account a WHERE a.balance<>(SELECT COALESCE(SUM(CASE side WHEN 'credit' THEN amount ELSE -amount END),0) FROM ledger_entry WHERE account_id=a.id)`).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("balance/entry mismatch %d: %v", n, err)
	}
}

func TestLedgerPostingAndStatement(t *testing.T) {
	f := newLedger(t, "ledger_posting")
	original, err := f.post(funding(1, 1, "20.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.post(transfer(1, 1, 2, "5.00")); err != nil {
		t.Fatal(err)
	}
	ledgerBalance(t, f, 1, "15.00")
	ledgerBalance(t, f, 2, "5.00")
	p := account.Posting{Source: account.Source{Type: account.BusinessReward, ID: 1, Action: account.ActionPost}, Entries: []account.Entry{
		account.Credit(1, model.KindRewardUSD, decimal.RequireFromString("2.35")), {System: account.SystemRewardExpense, Side: account.SideDebit, Amount: decimal.RequireFromString("2.35")},
		account.Credit(1, model.KindTaskPoints, decimal.RequireFromString("9007199254740993")), {System: account.SystemPointIssuance, Side: account.SideDebit, Amount: decimal.RequireFromString("9007199254740993")},
	}}
	if _, err = f.post(p); err != nil {
		t.Fatal(err)
	}
	balances, err := f.accounts.List(context.Background(), 1)
	if err != nil || balances[1].Balance != "2.35" || balances[2].Balance != "9007199254740993" {
		t.Fatal(balances, err)
	}
	page, err := f.accounts.Entries(context.Background(), 1, "", 0, 2)
	if err != nil || len(page.Entries) != 2 || page.NextID == 0 {
		t.Fatal(page, err)
	}
	next, err := f.accounts.Entries(context.Background(), 1, "", page.NextID, 2)
	if err != nil || len(next.Entries) != 2 || next.NextID != 0 || next.Entries[0].Side != string(account.SideDebit) || next.Entries[0].BalanceAfter.StringFixed(2) != "15.00" {
		t.Fatal(next, err)
	}
	other, err := f.accounts.Entries(context.Background(), 2, "", 0, 20)
	if err != nil || len(other.Entries) != 1 || other.Entries[0].BalanceAfter.StringFixed(2) != "5.00" {
		t.Fatal("counterpart leak", other, err)
	}
	points, err := f.accounts.Entries(context.Background(), 1, model.KindTaskPoints, 0, 20)
	if err != nil || len(points.Entries) != 1 || points.Entries[0].Amount.String() != "9007199254740993" {
		t.Fatal(points, err)
	}
	// Same source with changed amounts must be rejected, never silently replayed.
	if _, err = f.post(funding(1, 1, "99.00")); !errors.Is(err, account.ErrAlreadyPosted) {
		t.Fatal("duplicate", err)
	}
	ledgerBalance(t, f, 1, "15.00")
	correction := funding(2, 1, "1.00")
	correction.OriginalVoucherID = original
	corrected, err := f.post(correction)
	if err != nil {
		t.Fatal(err)
	}
	var ref int64
	if err = f.db.QueryRow("SELECT original_voucher_id FROM ledger_voucher WHERE id=?", corrected).Scan(&ref); err != nil || ref != original {
		t.Fatal(ref, err)
	}
	var originalAmount decimal.Decimal
	if err = f.db.QueryRow("SELECT amount FROM ledger_entry WHERE voucher_id=? AND line_no=1", original).Scan(&originalAmount); err != nil || originalAmount.StringFixed(2) != "20.00" {
		t.Fatal("original changed", err)
	}
	assertLedgerBalanced(t, f.db)
}

func TestLedgerRollbackAndAvailableBalance(t *testing.T) {
	f := newLedger(t, "ledger_rollback")
	self := funding(99, 1, "1.00")
	self.OriginalVoucherID = 1 // The next auto-increment in this fresh isolated schema.
	if _, err := f.post(self); err == nil {
		t.Fatal("voucher accepted itself as its original")
	}
	if _, err := f.post(transfer(1, 1, 2, "1.00")); !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal(err)
	}
	if ledgerCount(t, f.db, "user_account") != 0 || ledgerCount(t, f.db, "ledger_voucher") != 0 {
		t.Fatal("failed debit left rows")
	}
	// Fail the second entry in MySQL after balances and the first entry were written.
	mustExec(t, f.db, "ALTER TABLE ledger_entry ADD CONSTRAINT ck_test_late_entry CHECK(line_no<>2)")
	if _, err := f.post(funding(1, 1, "20.00")); err == nil {
		t.Fatal("late write failure accepted")
	}
	for _, table := range []string{"user_account", "ledger_voucher", "ledger_entry"} {
		if ledgerCount(t, f.db, table) != 0 {
			t.Fatal("partial posting", table)
		}
	}
	mustExec(t, f.db, "ALTER TABLE ledger_entry DROP CHECK ck_test_late_entry")
	failed := errors.New("business failed after posting")
	err := f.tx.TransactCtx(context.Background(), func(ctx context.Context, s sqlx.Session) error {
		if _, err := s.ExecCtx(ctx, "UPDATE user SET nickname='changed' WHERE id=1"); err != nil {
			return err
		}
		if _, err := f.accounts.Post(ctx, s, funding(1, 1, "20.00")); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) || ledgerCount(t, f.db, "ledger_entry") != 0 || ledgerCount(t, f.db, "user_account") != 0 {
		t.Fatal("outer rollback", err)
	}
	var name string
	if err = f.db.QueryRow("SELECT nickname FROM user WHERE id=1").Scan(&name); err != nil || name != "one" {
		t.Fatal(name, err)
	}
	if _, err = f.post(funding(1, 1, "20.00")); err != nil {
		t.Fatal(err)
	}
	// Held funds are a fixture for the 3.2 available-only debit boundary; 3.3 owns holds.
	mustExec(t, f.db, "UPDATE user_account SET held_balance=10 WHERE user_id=1")
	if _, err = f.post(transfer(1, 1, 2, "21.00")); !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal("spent held funds", err)
	}
	ledgerBalance(t, f, 1, "20.00")
	if ledgerCount(t, f.db, "user_account") != 1 {
		t.Fatal("failed transfer left first-credit target")
	}
	// An autocommit connection supplied inside a valid transaction is rejected.
	err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, _ sqlx.Session) error {
		_, err := f.accounts.Post(ctx, sqlx.NewSqlConnFromDB(f.db), funding(2, 1, "1.00"))
		return err
	})
	if err == nil {
		t.Fatal("different connection accepted")
	}
	assertLedgerBalanced(t, f.db)
}

func parallelPosts(f *ledgerFixture, posts []account.Posting) []error {
	results := make([]error, len(posts))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, p := range posts {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, results[i] = f.post(p) }()
	}
	close(start)
	wg.Wait()
	return results
}
func TestLedgerConcurrentFirstCreditDuplicateDebitAndTransfers(t *testing.T) {
	f := newLedger(t, "ledger_concurrent")
	posts := make([]account.Posting, 8)
	for i := range posts {
		posts[i] = funding(int64(i+1), 1, "1.00")
	}
	for _, err := range parallelPosts(f, posts) {
		if err != nil {
			t.Fatal("first credit", err)
		}
	}
	ledgerBalance(t, f, 1, "8.00")
	if ledgerCount(t, f.db, "user_account") != 1 {
		t.Fatal("multiple accounts")
	}
	for i := range posts {
		posts[i] = funding(100, 1, "2.00")
	}
	won := 0
	for _, err := range parallelPosts(f, posts) {
		if err == nil {
			won++
		} else if !errors.Is(err, account.ErrAlreadyPosted) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal("duplicate success count", won)
	}
	ledgerBalance(t, f, 1, "10.00")
	won = 0
	for _, err := range parallelPosts(f, []account.Posting{transfer(1, 1, 2, "7.00"), transfer(2, 1, 3, "7.00")}) {
		if err == nil {
			won++
		} else if !errors.Is(err, account.ErrInsufficientBalance) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal("debit success count", won)
	}
	ledgerBalance(t, f, 1, "3.00")
	if _, err := f.post(funding(101, 2, "10.00")); err != nil {
		t.Fatal(err)
	}
	for i := range posts {
		from, to := int64(1), int64(2)
		if i%2 == 1 {
			from, to = to, from
		}
		posts[i] = transfer(int64(200+i), from, to, "0.01")
	}
	for _, err := range parallelPosts(f, posts) {
		if err != nil {
			t.Fatal("opposite account order", err)
		}
	}
	ledgerBalance(t, f, 1, "3.00")
	assertLedgerBalanced(t, f.db)
}

func TestLedgerSchemaUpgradeAndConstraints(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "ledger_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(5); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id) VALUES(1)")
	mustExec(t, db, "INSERT INTO user_account(user_id,kind,balance) VALUES(1,'usd_balance',12.34)")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	fresh, _ := userDatabase(t, "ledger_fresh")
	_, _, full, err := testRun.NewDatabase(ctx, "ledger_full")
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
	for _, table := range []string{"ledger_voucher", "ledger_entry"} {
		var n, want, got string
		if err = full.QueryRow("SHOW CREATE TABLE "+table).Scan(&n, &want); err != nil {
			t.Fatal(err)
		}
		for _, other := range []*sql.DB{db, fresh} {
			if err = other.QueryRow("SHOW CREATE TABLE "+table).Scan(&n, &got); err != nil || got != want {
				t.Fatalf("%s schema differs: %v\n%s\n%s", table, err, got, want)
			}
		}
	}
	mustExec(t, db, "INSERT INTO ledger_voucher(id,business_type,business_id,action) VALUES(1,'deposit',1,'post')")
	for _, query := range []string{
		"INSERT INTO ledger_voucher(business_type,business_id,action) VALUES('deposit',1,'post')",
		"INSERT INTO ledger_voucher(business_type,business_id,action,original_voucher_id) VALUES('deposit',2,'post',999)",
		"INSERT INTO ledger_entry(voucher_id,line_no,system_code,unit,side,amount) VALUES(999,1,'usd_clearing','USD','debit',1)",
		"INSERT INTO ledger_entry(voucher_id,line_no,system_code,unit,side,amount) VALUES(1,1,'usd_clearing','USD','debit',0)",
		"INSERT INTO ledger_entry(voucher_id,line_no,system_code,unit,side,amount) VALUES(1,1,'usd_clearing','USD','debit',1.001)",
		"INSERT INTO ledger_entry(voucher_id,line_no,system_code,unit,side,amount) VALUES(1,1,'point_issuance','POINT','debit',1.5)",
		"INSERT INTO ledger_entry(voucher_id,line_no,account_id,system_code,unit,side,amount,balance_after) VALUES(1,1,1,'usd_clearing','USD','credit',1,13.34)",
		"INSERT INTO ledger_entry(voucher_id,line_no,account_id,unit,side,amount,balance_after) VALUES(1,1,1,'USD','credit',1,-1)",
	} {
		if _, err = db.Exec(query); err == nil {
			t.Fatal("constraint accepted", query)
		}
	}
	if err = m.Migrate(5); err != nil {
		t.Fatal(err)
	}
	var amount string
	if err = db.QueryRow("SELECT balance FROM user_account WHERE user_id=1").Scan(&amount); err != nil || !strings.HasPrefix(amount, "12.34") {
		t.Fatal("rollback changed old balance", amount, err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if ledgerCount(t, db, "ledger_voucher") != 0 {
		t.Fatal("upgrade invented history")
	}
	t.Log(fmt.Sprintf("business 5 -> %d -> 5 -> %d; existing account preserved", businessSchemaVersion, businessSchemaVersion))
}
