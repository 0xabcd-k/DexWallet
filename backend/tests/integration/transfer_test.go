//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	transfers "github.com/0xabcd-k/DexWallet/backend/logic/transfer"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type transferTx func(context.Context, func(context.Context, sqlx.Session) error) error

func (f transferTx) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	return f(ctx, fn)
}
func transferService(f *ledgerFixture, tx transfers.Transactor) *transfers.Service {
	return transfers.New(sqlx.NewSqlConnFromDB(f.db), tx, f.accounts)
}
func transferRequest(key string, to int64, amount string) transfers.Request {
	return transfers.Request{Key: key, RecipientID: to, Amount: decimal.RequireFromString(amount)}
}
func transferActor(id int64) userlogic.Actor { return userlogic.Actor{UserID: id, AuthVersion: 1} }
func assertTransferCount(t *testing.T, f *ledgerFixture, want int) {
	t.Helper()
	if n := ledgerCount(t, f.db, "transfer_order"); n != want {
		t.Fatalf("orders %d want %d", n, want)
	}
	var incomplete int
	err := f.db.QueryRow(`SELECT COUNT(*) FROM transfer_order t LEFT JOIN ledger_voucher v ON v.id=t.voucher_id WHERE v.id IS NULL OR v.business_type<>'transfer' OR v.business_id<>t.id OR v.action<>'post' OR (SELECT COUNT(*) FROM ledger_entry WHERE voucher_id=v.id)<>2`).Scan(&incomplete)
	if err != nil || incomplete != 0 {
		t.Fatal("partial transfer", incomplete, err)
	}
	assertLedgerBalanced(t, f.db)
}

func TestTransferIdempotencyAndParameterConflict(t *testing.T) {
	f := newLedger(t, "transfer_idempotency")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	s := transferService(f, f.tx)
	request := transferRequest("request_0123456789", 2, "10")
	const count = 12
	ids := make([]int64, count)
	failures := make([]error, count)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			order, err := s.Submit(context.Background(), transferActor(1), request)
			failures[i] = err
			if err == nil {
				ids[i] = order.Id
			}
		}()
	}
	close(start)
	wg.Wait()
	for i, id := range ids {
		if failures[i] != nil || id == 0 || id != ids[0] {
			t.Fatal(ids, failures)
		}
	}
	// Equivalent decimal encodings recover the exact same business fact.
	request.Amount = decimal.RequireFromString("10.00")
	original, err := s.Submit(context.Background(), transferActor(1), request)
	if err != nil || original.Id != ids[0] {
		t.Fatal(original, err)
	}
	found, err := s.FindByRequest(context.Background(), transferActor(1), request.Key)
	if err != nil || found.Id != original.Id || found.VoucherId != original.VoucherId || !found.CreatedAt.Equal(original.CreatedAt) {
		t.Fatal(found, err)
	}
	for _, changed := range []transfers.Request{transferRequest(request.Key, 2, "11"), transferRequest(request.Key, 3, "10"), transferRequest(request.Key, 999, "10")} {
		if _, err = s.Submit(context.Background(), transferActor(1), changed); !errors.Is(err, transfers.ErrRequestConflict) {
			t.Fatal("changed parameters", err)
		}
	}
	ledgerBalance(t, f, 1, "90.00")
	ledgerBalance(t, f, 2, "10.00")
	assertTransferCount(t, f, 1)
	if ledgerCount(t, f.db, "ledger_voucher") != 2 || ledgerCount(t, f.db, "ledger_entry") != 4 {
		t.Fatal("duplicate posting")
	}
	// Keys belong to a sender, not to a global cache namespace.
	reverse, err := s.Submit(context.Background(), transferActor(2), transferRequest(request.Key, 1, "1"))
	if err != nil || reverse.Id == original.Id {
		t.Fatal(reverse, err)
	}
	assertTransferCount(t, f, 2)
}

func TestTransferConcurrentSpendingAndReciprocalTransfers(t *testing.T) {
	f := newLedger(t, "transfer_concurrent")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	s := transferService(f, f.tx)
	failures := make([]error, 10)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range failures {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, failures[i] = s.Submit(context.Background(), transferActor(1), transferRequest(fmt.Sprintf("spending_key_%04d", i), 2, "30"))
		}()
	}
	close(start)
	wg.Wait()
	succeeded := 0
	for _, err := range failures {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, account.ErrInsufficientBalance) {
			t.Fatal(err)
		}
	}
	if succeeded != 3 {
		t.Fatal(succeeded)
	}
	ledgerBalance(t, f, 1, "10.00")
	ledgerBalance(t, f, 2, "90.00")
	assertTransferCount(t, f, 3)
	for i := range failures {
		wg.Add(1)
		go func() {
			defer wg.Done()
			from, to := int64(1), int64(2)
			if i%2 != 0 {
				from, to = to, from
			}
			_, failures[i] = s.Submit(context.Background(), transferActor(from), transferRequest(fmt.Sprintf("reciprocal_key_%04d", i), to, "1"))
		}()
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	ledgerBalance(t, f, 1, "10.00")
	ledgerBalance(t, f, 2, "90.00")
	assertTransferCount(t, f, 13)
}

func TestTransferRollbackAndOriginalKeyRetry(t *testing.T) {
	f := newLedger(t, "transfer_rollback")
	s := transferService(f, f.tx)
	request := transferRequest("rollback_key_012345", 2, "10")
	if order, err := s.Submit(context.Background(), transferActor(1), request); order != nil || !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal(order, err)
	}
	if ledgerCount(t, f.db, "user_account") != 0 || ledgerCount(t, f.db, "ledger_voucher") != 0 {
		t.Fatal("failed request left funds")
	}
	assertTransferCount(t, f, 0)
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"ledger_entry", "transfer_order"} {
		check := "line_no<>2"
		if table == "transfer_order" {
			check = "voucher_id IS NULL"
		}
		// Existing deposit lines must remain valid when installing the failure.
		if table == "ledger_entry" {
			check = "line_no<>2 OR account_id IS NULL"
		}
		mustExec(t, f.db, "ALTER TABLE "+table+" ADD CONSTRAINT ck_test_transfer_failure CHECK("+check+")")
		if order, err := s.Submit(context.Background(), transferActor(1), request); order != nil || err == nil {
			t.Fatal("late failure", order, err)
		}
		mustExec(t, f.db, "ALTER TABLE "+table+" DROP CHECK ck_test_transfer_failure")
		ledgerBalance(t, f, 1, "100.00")
		ledgerBalance(t, f, 2, "0.00")
		assertTransferCount(t, f, 0)
		if ledgerCount(t, f.db, "ledger_voucher") != 1 || ledgerCount(t, f.db, "ledger_entry") != 2 {
			t.Fatal("partial ledger")
		}
	}
	if _, err := s.Submit(context.Background(), transferActor(1), request); err != nil {
		t.Fatal(err)
	}
	ledgerBalance(t, f, 1, "90.00")
	ledgerBalance(t, f, 2, "10.00")
	assertTransferCount(t, f, 1)
}

func TestTransferCurrentAuthorityAndOwnerRecovery(t *testing.T) {
	f := newLedger(t, "transfer_authority")
	s := transferService(f, f.tx)
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	request := transferRequest("authority_key_012345", 2, "10")
	for _, tc := range []struct {
		sql   string
		actor userlogic.Actor
		want  error
	}{
		{"UPDATE user SET status='disabled' WHERE id=1", transferActor(1), userlogic.ErrDisabled},
		{"UPDATE user SET status='active',auth_version=2 WHERE id=1", transferActor(1), userlogic.ErrStaleSession},
		{"UPDATE user SET status='disabled' WHERE id=2", userlogic.Actor{UserID: 1, AuthVersion: 2}, transfers.ErrRecipientUnavailable},
	} {
		mustExec(t, f.db, tc.sql)
		if _, err := s.Submit(context.Background(), tc.actor, request); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	mustExec(t, f.db, "UPDATE user SET status='active',auth_version=1")
	if _, err := s.Submit(context.Background(), transferActor(1), transferRequest(request.Key, 999, "10")); !errors.Is(err, transfers.ErrRecipientUnavailable) {
		t.Fatal(err)
	}
	assertTransferCount(t, f, 0)
	order, err := s.Submit(context.Background(), transferActor(1), request)
	if err != nil {
		t.Fatal(err)
	}
	if found, err := s.FindByRequest(context.Background(), transferActor(3), request.Key); found != nil || !errors.Is(err, transfers.ErrNotFound) {
		t.Fatal("other sender read original", found, err)
	}
	mustExec(t, f.db, "UPDATE user SET status='disabled' WHERE id=2")
	replay, err := s.Submit(context.Background(), transferActor(1), request)
	if err != nil || replay.Id != order.Id {
		t.Fatal("historical result changed with recipient status", replay, err)
	}
	mustExec(t, f.db, "UPDATE user SET auth_version=2 WHERE id=1")
	if _, err := s.FindByRequest(context.Background(), transferActor(1), request.Key); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal(err)
	}
	assertTransferCount(t, f, 1)
}

func TestTransferCommitResponseLossAndUnknownRecovery(t *testing.T) {
	for _, committed := range []bool{true, false} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f := newLedger(t, fmt.Sprintf("transfer_unknown_%t", committed))
			if _, err := f.post(funding(1, 1, "100")); err != nil {
				t.Fatal(err)
			}
			lost := errors.New("commit acknowledgement lost")
			calls := 0
			tx := transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
				calls++
				err := f.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
					if err := fn(ctx, session); err != nil {
						return err
					}
					if !committed {
						return lost
					}
					return nil
				})
				if err != nil && !errors.Is(err, lost) {
					return err
				}
				return &transaction.Failure{Phase: "commit", Cause: lost}
			})
			request := transferRequest("unknown_key_012345", 2, "10")
			result, err := transferService(f, tx).Submit(context.Background(), transferActor(1), request)
			if calls != 1 {
				t.Fatal("unknown commit retried", calls)
			}
			if committed {
				if err != nil || result == nil {
					t.Fatal(result, err)
				}
			} else {
				if result != nil || !errors.Is(err, transfers.ErrOutcomeUnknown) || !errors.Is(err, lost) {
					t.Fatal(result, err)
				}
				assertTransferCount(t, f, 0)
			}
			// Same-key recovery is safe whether the original actually committed or rolled back.
			restored, err := transferService(f, f.tx).Submit(context.Background(), transferActor(1), request)
			if err != nil || (committed && restored.Id != result.Id) {
				t.Fatal(restored, err)
			}
			ledgerBalance(t, f, 1, "90.00")
			ledgerBalance(t, f, 2, "10.00")
			assertTransferCount(t, f, 1)
		})
	}
}

// A lost acknowledgement may arrive while the original transaction is still
// committing. A primary read miss must not turn that operation into a new one.
func TestTransferRecoveryWhileOriginalIsUncommitted(t *testing.T) {
	f := newLedger(t, "transfer_pending_commit")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	tx := transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
		go func() {
			finished <- f.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
				err := fn(ctx, session)
				close(ready)
				if err != nil {
					return err
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
		return &transaction.Failure{Phase: "commit", Cause: errors.New("connection response lost while committing")}
	})
	request := transferRequest("pending_key_012345", 2, "10")
	result, err := transferService(f, tx).Submit(ctx, transferActor(1), request)
	if result != nil || !errors.Is(err, transfers.ErrOutcomeUnknown) {
		t.Fatal(result, err)
	}
	plain := transferService(f, f.tx)
	if _, err = plain.FindByRequest(ctx, transferActor(1), request.Key); !errors.Is(err, transfers.ErrNotFound) {
		t.Fatal(err)
	}
	waiting, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	result, err = plain.Submit(waiting, transferActor(1), request)
	stop()
	if result != nil || err == nil {
		t.Fatal("pending duplicate did not wait", result, err)
	}
	unblock()
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	result, err = plain.Submit(ctx, transferActor(1), request)
	if err != nil || result == nil {
		t.Fatal(result, err)
	}
	ledgerBalance(t, f, 1, "90.00")
	ledgerBalance(t, f, 2, "10.00")
	assertTransferCount(t, f, 1)
}

func TestTransferDeadlockRetryRollsBackWholeAttempt(t *testing.T) {
	f := newLedger(t, "transfer_retry")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tx := transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
		calls++
		return f.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			if err := fn(ctx, session); err != nil {
				return err
			}
			if calls == 1 {
				return &mysql.MySQLError{Number: 1213, Message: "injected deadlock at transaction boundary"}
			}
			return nil
		})
	})
	if _, err := transferService(f, tx).Submit(context.Background(), transferActor(1), transferRequest("deadlock_key_012345", 2, "10")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	ledgerBalance(t, f, 1, "90.00")
	ledgerBalance(t, f, 2, "10.00")
	assertTransferCount(t, f, 1)
}

func TestTransferLockTimeoutAndCancellation(t *testing.T) {
	f := newLedger(t, "transfer_wait")
	if _, err := f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	locker, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var id int64
	if err = locker.QueryRow("SELECT id FROM user_account WHERE user_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tx := transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
		calls++
		return f.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			var old int
			if err := session.QueryRowCtx(ctx, &old, "SELECT @@session.innodb_lock_wait_timeout"); err != nil {
				return err
			}
			if _, err := session.ExecCtx(ctx, "SET SESSION innodb_lock_wait_timeout=1"); err != nil {
				return err
			}
			defer session.ExecCtx(context.Background(), "SET SESSION innodb_lock_wait_timeout=?", old)
			return fn(ctx, session)
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := transferRequest("waiting_key_012345", 2, "10")
	result, err := transferService(f, tx).Submit(ctx, transferActor(1), request)
	var dbErr *mysql.MySQLError
	if result != nil || calls != 1 || !errors.Is(err, transfers.ErrBusy) || !errors.As(err, &dbErr) || dbErr.Number != 1205 {
		t.Fatal(result, err, calls)
	}
	cancelled, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	result, err = transferService(f, f.tx).Submit(cancelled, transferActor(1), request)
	if result != nil || err == nil || cancelled.Err() == nil {
		t.Fatal("cancelled transfer", result, err)
	}
	if err = locker.Commit(); err != nil {
		t.Fatal(err)
	}
	ledgerBalance(t, f, 1, "100.00")
	ledgerBalance(t, f, 2, "0.00")
	assertTransferCount(t, f, 0)
	if _, err = transferService(f, f.tx).Submit(context.Background(), transferActor(1), request); err != nil {
		t.Fatal(err)
	}
	assertTransferCount(t, f, 1)
}

func TestTransferSchemaUpgradeAndHistoryGuard(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "transfer_schema")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(7); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO user(id) VALUES(1),(2)")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	_, _, full, err := testRun.NewDatabase(ctx, "transfer_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, source := range []string{"user/tables.sql", "user/constraints.sql", "account/tables.sql", "account/constraints.sql", "transfer/tables.sql", "transfer/constraints.sql"} {
		ddl, err := os.ReadFile("../../manifest/sql/ddl/" + source)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	var table, want, got string
	if err = full.QueryRow("SHOW CREATE TABLE transfer_order").Scan(&table, &want); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SHOW CREATE TABLE transfer_order").Scan(&table, &got); err != nil || got != want {
		t.Fatalf("DDL differs: %v\n%s\n%s", err, got, want)
	}
	if err = m.Migrate(7); err != nil {
		t.Fatal("empty rollback", err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{"(1,1,'schema_key_012345',10)", "(1,2,'schema_key_012345',0)", "(1,2,'schema_key_012345',1.001)", "(1,999,'schema_key_012345',10)", "(1,2,'short',10)"} {
		if _, err = db.Exec("INSERT INTO transfer_order(sender_id,recipient_id,request_key,amount) VALUES" + values); err == nil {
			t.Fatal("constraint accepted", values)
		}
	}
	f := &ledgerFixture{db: db, accounts: account.New(nil), tx: transaction.New(db)}
	if _, err = f.post(funding(1, 1, "100")); err != nil {
		t.Fatal(err)
	}
	s := transferService(f, f.tx)
	order, err := s.Submit(ctx, transferActor(1), transferRequest("schema_key_012345", 2, "10"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO transfer_order(sender_id,recipient_id,request_key,amount) VALUES(1,2,'schema_key_012345',10)"); err == nil {
		t.Fatal("duplicate request accepted")
	}
	if err = m.Migrate(7); err == nil || !strings.Contains(err.Error(), "rollback_guard") {
		t.Fatal("history erased", err)
	}
	found, err := s.FindByRequest(ctx, transferActor(1), order.RequestKey)
	if err != nil || found.Id != order.Id {
		t.Fatal(found, err)
	}
	assertTransferCount(t, f, 1)
}
