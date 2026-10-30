//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func reserve(f *ledgerFixture, source, user int64, amount string) (id int64, err error) {
	err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		id, err = f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: user, BusinessType: account.BusinessWithdrawal, BusinessID: source, Amount: decimal.RequireFromString(amount)})
		return err
	})
	return
}
func resolve(f *ledgerFixture, id int64, consume bool, amount string) (err error) {
	return f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		if consume {
			_, err = f.accounts.Consume(ctx, tx, id, []account.Entry{{System: account.SystemUSDClearing, Side: account.SideCredit, Amount: decimal.RequireFromString(amount)}})
		} else {
			_, err = f.accounts.Release(ctx, tx, id, model.HoldCancelled)
		}
		return err
	})
}
func heldBalance(t *testing.T, f *ledgerFixture, available, held string) {
	t.Helper()
	rows, err := f.accounts.List(context.Background(), 1)
	if err != nil || rows[0].Balance != available || rows[0].HeldBalance != held {
		t.Fatalf("balances %+v: %v; want %s/%s", rows, err, available, held)
	}
}
func assertHoldsConsistent(t *testing.T, f *ledgerFixture) {
	t.Helper()
	assertLedgerBalanced(t, f.db)
	var n int
	err := f.db.QueryRow(`SELECT COUNT(*) FROM user_account a WHERE held_balance<>(SELECT COALESCE(SUM(amount),0) FROM account_hold h WHERE h.account_id=a.id AND h.status='held') OR held_balance<>(SELECT COALESCE(SUM(CASE side WHEN 'credit' THEN amount ELSE -amount END),0) FROM ledger_entry WHERE account_id=a.id AND balance_part='held')`).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("held amount/records/entries mismatch %d: %v", n, err)
	}
}
func TestHoldsLifecycleAndStatement(t *testing.T) {
	f := newLedger(t, "holds_lifecycle")
	if _, err := f.post(funding(1, 1, "100.00")); err != nil {
		t.Fatal(err)
	}
	id, err := reserve(f, 1, 1, "30.00")
	if err != nil {
		t.Fatal(err)
	}
	heldBalance(t, f, "70.00", "30.00")
	assertHoldsConsistent(t, f)
	page, err := f.accounts.Entries(context.Background(), 1, model.KindUSD, 0, 20)
	if err != nil || len(page.Entries) != 2 || account.EntryActivity(page.Entries[0]) != model.HoldHeld || page.Entries[0].BalanceAfter.StringFixed(2) != "70.00" {
		t.Fatal(page, err)
	}
	if err = resolve(f, id, true, "29.00"); !errors.Is(err, account.ErrInvalidHold) {
		t.Fatal("partial consumption", err)
	}
	heldBalance(t, f, "70.00", "30.00")
	if err = resolve(f, id, true, "30.00"); err != nil {
		t.Fatal(err)
	}
	heldBalance(t, f, "70.00", "0.00")
	for _, consume := range []bool{true, false} {
		if err = resolve(f, id, consume, "30.00"); !errors.Is(err, account.ErrHoldResolved) {
			t.Fatal("repeat resolution", err)
		}
	}
	page, err = f.accounts.Entries(context.Background(), 1, "", 0, 20)
	if err != nil || len(page.Entries) != 3 || account.EntryActivity(page.Entries[0]) != model.HoldConsumed || page.Entries[0].BalancePart != model.BalanceHeld || !page.Entries[0].BalanceAfter.IsZero() {
		t.Fatal(page, err)
	}
	second, err := reserve(f, 2, 1, "10.00")
	if err != nil {
		t.Fatal(err)
	}
	if err = resolve(f, second, false, ""); err != nil {
		t.Fatal(err)
	}
	heldBalance(t, f, "70.00", "0.00")
	page, err = f.accounts.Entries(context.Background(), 1, "", 0, 20)
	if err != nil || len(page.Entries) != 5 || account.EntryActivity(page.Entries[0]) != model.HoldReleased {
		t.Fatal(page, err)
	}
	// Ordinary debits only see the remaining available portion.
	third, err := reserve(f, 3, 1, "60.00")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.post(transfer(1, 1, 2, "11.00")); !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal("spent a reservation", err)
	}
	if _, err = f.post(transfer(2, 1, 2, "10.00")); err != nil {
		t.Fatal(err)
	}
	if err = resolve(f, third, false, ""); err != nil {
		t.Fatal(err)
	}
	holds, err := f.accounts.Holds(context.Background(), 1, "", 0, 1)
	if err != nil || len(holds.Holds) != 1 || holds.NextID != third || holds.Holds[0].Reason != model.HoldCancelled || !holds.Holds[0].ResolvedAt.Valid {
		t.Fatal(holds, err)
	}
	next, err := f.accounts.Holds(context.Background(), 1, "", holds.NextID, 20)
	if err != nil || len(next.Holds) != 2 || next.NextID != 0 {
		t.Fatal(next, err)
	}
	consumed, err := f.accounts.Holds(context.Background(), 1, model.HoldConsumed, 0, 20)
	if err != nil || len(consumed.Holds) != 1 || consumed.Holds[0].Id != id {
		t.Fatal(consumed, err)
	}
	other, err := f.accounts.Holds(context.Background(), 2, "", 0, 20)
	if err != nil || len(other.Holds) != 0 {
		t.Fatal("owner leak", other, err)
	}
	var original int64
	if err = f.db.QueryRow("SELECT original_voucher_id FROM ledger_voucher WHERE id=?", consumed.Holds[0].ResolvedVoucherId.Int64).Scan(&original); err != nil || original != consumed.Holds[0].HoldVoucherId {
		t.Fatal(original, err)
	}
	assertHoldsConsistent(t, f)
}
func TestHoldsConcurrentReserveAndResolve(t *testing.T) {
	f := newLedger(t, "holds_concurrent")
	if _, err := f.post(funding(1, 1, "100.00")); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make([]error, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, results[i] = reserve(f, 1, 1, "30.00") }()
	}
	close(start)
	wg.Wait()
	won := 0
	for _, err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, account.ErrAlreadyHeld) {
			t.Fatal(err)
		}
	}
	if won != 1 || ledgerCount(t, f.db, "account_hold") != 1 {
		t.Fatal("duplicate hold", won)
	}
	if _, err := reserve(f, 1, 1, "50.00"); !errors.Is(err, account.ErrAlreadyHeld) {
		t.Fatal("changed request accepted", err)
	}
	heldBalance(t, f, "70.00", "30.00")
	row, err := model.NewAccountHoldModel(sqlx.NewSqlConnFromDB(f.db)).FindOneByBusinessTypeBusinessId(context.Background(), string(account.BusinessWithdrawal), 1)
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	results = make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results[i] = resolve(f, row.Id, i == 0, "30.00") }()
	}
	close(start)
	wg.Wait()
	won = 0
	for _, err = range results {
		if err == nil {
			won++
		} else if !errors.Is(err, account.ErrHoldResolved) {
			t.Fatal(err)
		}
	}
	if won != 1 || ledgerCount(t, f.db, "ledger_voucher") != 3 {
		t.Fatal("non-exclusive resolution", won)
	}
	start = make(chan struct{})
	results = make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, results[i] = reserve(f, int64(i+2), 1, "60.00") }()
	}
	close(start)
	wg.Wait()
	won = 0
	for _, err = range results {
		if err == nil {
			won++
		} else if !errors.Is(err, account.ErrInsufficientBalance) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal("concurrent overspend", results)
	}
	assertHoldsConsistent(t, f)
}
func TestHoldsValidationAndRollback(t *testing.T) {
	f := newLedger(t, "holds_rollback")
	if _, err := reserve(f, 1, 1, "1.00"); !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal(err)
	}
	if ledgerCount(t, f.db, "user_account") != 0 {
		t.Fatal("failed hold created account")
	}
	if _, err := f.post(funding(1, 1, "100.00")); err != nil {
		t.Fatal(err)
	}
	for _, amount := range []string{"0", "-1", "1.001", "1000000000000000000"} {
		if _, err := reserve(f, 1, 1, amount); !errors.Is(err, account.ErrInvalidHold) {
			t.Fatal(amount, err)
		}
	}
	for _, action := range []account.Action{account.ActionHold, account.ActionConsume, account.ActionRelease} {
		p := funding(2, 1, "1.00")
		p.Source.Action = action
		if _, err := f.post(p); !errors.Is(err, account.ErrInvalidPosting) {
			t.Fatal("bypassed holds", err)
		}
	}
	failed := errors.New("business failed")
	err := f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		if _, err := f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: 1, BusinessType: account.BusinessReward, BusinessID: 1, Amount: decimal.NewFromInt(1)}); !errors.Is(err, account.ErrInvalidHold) {
			t.Fatal("reward hold accepted", err)
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	// Fail after the voucher, balances and first ledger entry were written.
	mustExec(t, f.db, "ALTER TABLE ledger_entry ADD CONSTRAINT ck_test_hold_entry CHECK(line_no<>2 OR balance_part<>'held')")
	if _, err = reserve(f, 1, 1, "30.00"); err == nil {
		t.Fatal("failed entry accepted")
	}
	mustExec(t, f.db, "ALTER TABLE ledger_entry DROP CHECK ck_test_hold_entry")
	heldBalance(t, f, "100.00", "0.00")
	if ledgerCount(t, f.db, "account_hold") != 0 || ledgerCount(t, f.db, "ledger_voucher") != 1 {
		t.Fatal("partial hold")
	}
	var id int64
	err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		var err error
		id, err = f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: 1, BusinessType: account.BusinessWithdrawal, BusinessID: 1, Amount: decimal.NewFromInt(30)})
		if err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	heldBalance(t, f, "100.00", "0.00")
	id, err = reserve(f, 1, 1, "30.00")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.db, "ALTER TABLE account_hold ADD CONSTRAINT ck_test_hold_terminal CHECK(status='held')")
	if err = resolve(f, id, true, "30.00"); err == nil {
		t.Fatal("late hold state failure accepted")
	}
	mustExec(t, f.db, "ALTER TABLE account_hold DROP CHECK ck_test_hold_terminal")
	heldBalance(t, f, "70.00", "30.00")
	if ledgerCount(t, f.db, "ledger_voucher") != 2 {
		t.Fatal("partial consume")
	}
	err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		if _, err := f.accounts.Release(ctx, tx, id, model.HoldRejected); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	heldBalance(t, f, "70.00", "30.00")
	if err = resolve(f, id, false, ""); err != nil {
		t.Fatal(err)
	}
	if err = resolve(f, id+999, true, "30.00"); !errors.Is(err, account.ErrHoldNotFound) {
		t.Fatal(err)
	}
	assertHoldsConsistent(t, f)
}
func TestHoldsDatabaseLockTimeoutAndCancellation(t *testing.T) {
	f := newLedger(t, "holds_wait")
	if _, err := f.post(funding(1, 1, "100.00")); err != nil {
		t.Fatal(err)
	}
	locker, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var id int64
	if err = locker.QueryRow("SELECT id FROM user_account WHERE user_id=1 AND kind='usd_balance' FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = f.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		var old int
		if err := tx.QueryRowCtx(ctx, &old, "SELECT @@session.innodb_lock_wait_timeout"); err != nil {
			return err
		}
		if _, err := tx.ExecCtx(ctx, "SET SESSION innodb_lock_wait_timeout=1"); err != nil {
			return err
		}
		defer tx.ExecCtx(context.Background(), "SET SESSION innodb_lock_wait_timeout=?", old)
		_, err := f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: 1, BusinessType: account.BusinessWithdrawal, BusinessID: 1, Amount: decimal.NewFromInt(30)})
		return err
	})
	if !errors.Is(err, account.ErrAccountBusy) {
		t.Fatal("lock wait not distinguished", err)
	}
	cancelled, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	err = f.tx.TransactCtx(cancelled, func(ctx context.Context, tx sqlx.Session) error {
		_, err := f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: 1, BusinessType: account.BusinessWithdrawal, BusinessID: 2, Amount: decimal.NewFromInt(30)})
		return err
	})
	if err == nil || cancelled.Err() == nil {
		t.Fatal("cancelled wait succeeded", err)
	}
	if err = locker.Commit(); err != nil {
		t.Fatal(err)
	}
	heldBalance(t, f, "100.00", "0.00")
	if ledgerCount(t, f.db, "account_hold") != 0 || ledgerCount(t, f.db, "ledger_voucher") != 1 {
		t.Fatal("waiting write left money changes")
	}
	if _, err = reserve(f, 1, 1, "30.00"); err != nil {
		t.Fatal("lock release did not allow write", err)
	}
	assertHoldsConsistent(t, f)
}

func TestHoldsSchemaUpgradeAndHistoryGuard(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "holds_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(6); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id) VALUES(1)")
	mustExec(t, db, "INSERT INTO user_account(id,user_id,kind,balance) VALUES(1,1,'usd_balance',10)")
	mustExec(t, db, "INSERT INTO ledger_voucher(id,business_type,business_id,action) VALUES(1,'deposit',1,'post')")
	mustExec(t, db, "INSERT INTO ledger_entry(voucher_id,line_no,account_id,unit,side,amount,balance_after) VALUES(1,1,1,'USD','credit',10,10)")
	mustExec(t, db, "INSERT INTO ledger_entry(voucher_id,line_no,system_code,unit,side,amount) VALUES(1,2,'usd_clearing','USD','debit',10)")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	var part string
	if err = db.QueryRow("SELECT balance_part FROM ledger_entry WHERE account_id=1").Scan(&part); err != nil || part != model.BalanceAvailable {
		t.Fatal("old entry changed", part, err)
	}
	_, _, full, err := testRun.NewDatabase(ctx, "holds_full")
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
	for _, table := range []string{"ledger_entry", "account_hold"} {
		var name, want, got string
		if err = full.QueryRow("SHOW CREATE TABLE "+table).Scan(&name, &want); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow("SHOW CREATE TABLE "+table).Scan(&name, &got); err != nil {
			t.Fatal(err)
		}
		got = strings.ReplaceAll(got, " AUTO_INCREMENT=3", "")
		// MySQL may rewrite ASCII string introducers on ALTER without changing semantics.
		got = strings.ReplaceAll(got, "_ascii'", "_utf8mb4'")
		if got != want {
			t.Fatalf("schema mismatch %s\n%s\n%s", table, got, want)
		}
	}
	if err = m.Migrate(6); err != nil {
		t.Fatal("empty holds rollback", err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	f := &ledgerFixture{db: db, accounts: account.New(model.NewUserAccountModel(sqlx.NewSqlConnFromDB(db))), tx: nil}
	// Use the same outer transaction runner as production commands.
	f.tx = transaction.New(db)
	if _, err = reserve(f, 1, 1, "3.00"); err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(6); err == nil {
		t.Fatal("rollback erased hold history")
	}
	if ledgerCount(t, db, "account_hold") != 1 {
		t.Fatal("history lost")
	}
	assertHoldsConsistent(t, f)
}

func TestHoldsRemainReleasableAtAmountLimit(t *testing.T) {
	f := newLedger(t, "holds_capacity")
	if _, err := f.post(funding(1, 1, "999999999999999999.99")); err != nil {
		t.Fatal(err)
	}
	id, err := reserve(f, 1, 1, "0.01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.post(funding(2, 1, "0.01")); !errors.Is(err, account.ErrInvalidPosting) {
		t.Fatal("credit consumed reserved release capacity", err)
	}
	heldBalance(t, f, "999999999999999999.98", "0.01")
	if _, err = f.db.Exec("UPDATE user_account SET balance=balance+0.01 WHERE user_id=1"); err == nil {
		t.Fatal("database accepted unreleasable total")
	}
	if err = resolve(f, id, false, ""); err != nil {
		t.Fatal("valid hold cannot release", err)
	}
	heldBalance(t, f, "999999999999999999.99", "0.00")
	assertHoldsConsistent(t, f)
}

func TestHoldsPaymentCounterparts(t *testing.T) {
	f := newLedger(t, "holds_counterparts")
	if _, err := f.post(funding(1, 1, "10.00")); err != nil {
		t.Fatal(err)
	}
	var id int64
	err := f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		var err error
		id, err = f.accounts.Hold(ctx, tx, account.HoldRequest{UserID: 1, BusinessType: account.BusinessPayment, BusinessID: 1, Amount: decimal.NewFromInt(4)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, credits := range [][]account.Entry{
		{account.Credit(1, model.KindUSD, decimal.NewFromInt(4))},
		{account.Credit(2, model.KindRewardUSD, decimal.NewFromInt(4))},
		{account.Credit(2, model.KindTaskPoints, decimal.NewFromInt(4))},
		{{System: account.SystemPointIssuance, Side: account.SideCredit, Amount: decimal.NewFromInt(4)}},
		{account.Debit(2, model.KindUSD, decimal.NewFromInt(4))},
	} {
		err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
			_, err := f.accounts.Consume(ctx, tx, id, credits)
			return err
		})
		if err == nil {
			t.Fatal("invalid counterpart accepted", credits)
		}
	}
	heldBalance(t, f, "6.00", "4.00")
	if ledgerCount(t, f.db, "ledger_voucher") != 2 {
		t.Fatal("invalid counterpart left a voucher")
	}
	err = f.tx.TransactCtx(context.Background(), func(ctx context.Context, tx sqlx.Session) error {
		_, err := f.accounts.Consume(ctx, tx, id, []account.Entry{account.Credit(2, model.KindUSD, decimal.RequireFromString("1.25")), {System: account.SystemUSDClearing, Side: account.SideCredit, Amount: decimal.RequireFromString("2.75")}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerBalance(t, f, 2, "1.25")
	page, err := f.accounts.Entries(context.Background(), 2, "", 0, 20)
	if err != nil || len(page.Entries) != 1 || account.EntryActivity(page.Entries[0]) != account.ActivityPosted || page.Entries[0].BalancePart != model.BalanceAvailable {
		t.Fatal("recipient mislabelled as held", page, err)
	}
	assertHoldsConsistent(t, f)
}
func TestHoldsUpgradeRejectsUnattributedHeldFunds(t *testing.T) {
	name, dsn, db, err := testRun.NewDatabase(context.Background(), "holds_legacy")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(6); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id) VALUES(1)")
	mustExec(t, db, "INSERT INTO user_account(user_id,kind,balance,held_balance) VALUES(1,'usd_balance',10,1)")
	if err = m.Up(); err == nil {
		t.Fatal("upgrade invented a source for old held funds")
	}
	var held decimal.Decimal
	if err = db.QueryRow("SELECT held_balance FROM user_account WHERE user_id=1").Scan(&held); err != nil || !held.Equal(decimal.NewFromInt(1)) {
		t.Fatal("upgrade lost old held funds", held, err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='account_hold'").Scan(&count); err != nil || count != 0 {
		t.Fatal("upgrade passed guard", count, err)
	}
}
