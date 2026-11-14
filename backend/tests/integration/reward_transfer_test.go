//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	rt "github.com/0xabcd-k/DexWallet/backend/logic/rewardtransfer"
	"github.com/0xabcd-k/DexWallet/backend/logic/risk"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/rewardtransfer"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
)

type riskFunc func(context.Context, risk.Request) (risk.Decision, error)

func (f riskFunc) Assess(c context.Context, r risk.Request) (risk.Decision, error) { return f(c, r) }
func rtService(db *sql.DB, tx rt.Transactor, r risk.Assessor) *rt.Service {
	c := sqlx.NewSqlConnFromDB(db)
	return rt.New(c, tx, account.New(accounts.NewUserAccountModel(c)), r)
}
func rtRisk(action string) risk.Assessor {
	return riskFunc(func(context.Context, risk.Request) (risk.Decision, error) {
		return risk.Decision{Action: action, Version: "test_v1", Reason: "test risk decision"}, nil
	})
}
func rtFund(t *testing.T, db *sql.DB, id int64, pro bool, amount string) {
	t.Helper()
	if pro {
		mustExec(t, db, "INSERT INTO user_reward_state(user_id,pro_progress,pro_unlocked_at) VALUES(?,2000,UTC_TIMESTAMP(6))", id)
	}
	a := account.New(accounts.NewUserAccountModel(sqlx.NewSqlConnFromDB(db)))
	v := decimal.RequireFromString(amount)
	if err := transaction.New(db).TransactCtx(context.Background(), func(c context.Context, s sqlx.Session) error {
		_, err := a.Post(c, s, account.Posting{Source: account.Source{Type: account.BusinessReward, ID: 980000 + id, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(id, accounts.KindRewardUSD, v), {System: account.SystemRewardExpense, Side: account.SideDebit, Amount: v}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func rtReview(db *sql.DB, s *rt.Service, id int64, decision string) (*model.RewardTransfer, error) {
	var row *model.RewardTransfer
	err := transaction.New(db).TransactCtx(context.Background(), func(c context.Context, tx sqlx.Session) error {
		var err error
		row, _, err = s.ReviewInSession(c, tx, id, 1, decision, "test review")
		return err
	})
	return row, err
}
func TestRewardTransferEligibilityRiskAndRecovery(t *testing.T) {
	c := context.Background()
	db, u := userDatabase(t, "reward_transfer_paths")
	a := rewardActor(t, u, 111001)
	other := rewardActor(t, u, 111002)
	rtFund(t, db, a.UserID, true, "12")
	rtFund(t, db, other.UserID, false, "4")
	var calls atomic.Int64
	assessor := riskFunc(func(context.Context, risk.Request) (risk.Decision, error) {
		calls.Add(1)
		return risk.Decision{Action: risk.Allow, Version: "v1"}, nil
	})
	s := rtService(db, transaction.New(db), assessor)
	_, err := s.Submit(c, other, rt.Request{Key: "reward_no_pro_request", Amount: "2"})
	taskCode(t, err, apperror.RewardTransferProRequired)
	_, err = s.Submit(c, a, rt.Request{Key: "reward_low_amount_key", Amount: "1.99"})
	taskCode(t, err, apperror.RewardTransferMinimum)
	if calls.Load() != 0 {
		t.Fatal("risk called before business eligibility")
	}
	// Identity replacement freezes outgoing payments but permits reward transfers.
	mustExec(t, db, "UPDATE user SET payment_restricted_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY) WHERE id=?", a.UserID)
	req := rt.Request{Key: "reward_auto_transfer1", Amount: "2.01"}
	first, err := s.Submit(c, a, req)
	if err != nil || first.Status != rt.Completed {
		t.Fatal(first, err)
	}
	repeated, err := s.Submit(c, a, req)
	if err != nil || repeated.Id != first.Id || calls.Load() != 1 {
		t.Fatal(repeated, err, calls.Load())
	}
	_, err = s.Submit(c, a, rt.Request{Key: req.Key, Amount: "3"})
	taskCode(t, err, apperror.RewardTransferRequestConflict)
	_, err = s.Detail(c, other, first.Id)
	taskCode(t, err, apperror.NotFound)
	unavailable := rtService(db, transaction.New(db), nil)
	if _, err = unavailable.Submit(c, a, req); err != nil {
		t.Fatal("original result unavailable", err)
	}
	_, err = unavailable.Submit(c, a, rt.Request{Key: "reward_risk_unavailable", Amount: "2"})
	taskCode(t, err, apperror.RewardRiskUnavailable)
	failed := rtService(db, transaction.New(db), riskFunc(func(context.Context, risk.Request) (risk.Decision, error) {
		return risk.Decision{}, errors.New("timeout")
	}))
	_, err = failed.Submit(c, a, rt.Request{Key: "reward_risk_failed01", Amount: "2"})
	taskCode(t, err, apperror.RewardRiskUnavailable)
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "9.99")
	taskBalance(t, db, a.UserID, accounts.KindUSD, "2.01")
	if ledgerCount(t, db, "reward_transfer") != 1 {
		t.Fatal("failed risk left application")
	}
	lost := rtService(db, lostRewardCommit{transaction.New(db)}, rtRisk(risk.Allow))
	_, err = lost.Submit(c, a, rt.Request{Key: "reward_lost_response", Amount: "2"})
	if err != nil {
		t.Fatal(err)
	}
	unknown := rtService(db, transferTx(func(context.Context, func(context.Context, sqlx.Session) error) error {
		return &transaction.Failure{Phase: "commit", Cause: errors.New("unknown")}
	}), rtRisk(risk.Allow))
	_, err = unknown.Submit(c, a, rt.Request{Key: "reward_unknown_missing", Amount: "2"})
	if !errors.Is(err, rt.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	rows, next, err := s.List(c, a, 0, 1)
	if err != nil || len(rows) != 1 || next == 0 {
		t.Fatal(rows, next, err)
	}
	mustExec(t, db, "UPDATE user SET auth_version=auth_version+1 WHERE id=?", a.UserID)
	_, err = s.Submit(c, a, req)
	if !errors.Is(err, users.ErrStaleSession) {
		t.Fatal(err)
	}
	assertLedgerBalanced(t, db)
}
func TestRewardTransferManualReviewAndRollback(t *testing.T) {
	c := context.Background()
	db, u := userDatabase(t, "reward_transfer_review")
	a := rewardActor(t, u, 112001)
	rtFund(t, db, a.UserID, true, "10")
	mustExec(t, db, "INSERT INTO admin_user(id,email,password_hash,must_change_password) VALUES(1,'review@example.com','test-fixture',0)")
	s := rtService(db, transaction.New(db), rtRisk(risk.Review))
	first, err := s.Submit(c, a, rt.Request{Key: "reward_manual_request1", Amount: "3"})
	if err != nil || first.Status != rt.PendingReview {
		t.Fatal(first, err)
	}
	state, err := s.State(c, a)
	if err != nil || state.PendingUSD != "3.00" || state.RewardUSD != "7.00" {
		t.Fatal(state, err)
	}
	// Failure after posting must roll back the balance, voucher and status together.
	mustExec(t, db, "ALTER TABLE reward_transfer ADD CONSTRAINT test_rt_fault CHECK (resolution_voucher_id IS NULL)")
	if _, err = rtReview(db, s, first.Id, rt.Approve); err == nil {
		t.Fatal("fault ignored")
	}
	taskBalance(t, db, a.UserID, accounts.KindUSD, "0")
	mustExec(t, db, "ALTER TABLE reward_transfer DROP CHECK test_rt_fault")
	// Already accepted applications continue when account activity is disabled.
	mustExec(t, db, "UPDATE user SET status='disabled' WHERE id=?", a.UserID)
	if _, err = rtReview(db, s, first.Id, rt.Approve); err != nil {
		t.Fatal(err)
	}
	if _, err = rtReview(db, s, first.Id, rt.Approve); err != nil {
		t.Fatal("repeat approval", err)
	}
	_, err = rtReview(db, s, first.Id, rt.Reject)
	taskCode(t, err, apperror.Conflict)
	mustExec(t, db, "UPDATE user SET status='active' WHERE id=?", a.UserID)
	second, err := s.Submit(c, a, rt.Request{Key: "reward_manual_request2", Amount: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rtReview(db, s, second.Id, rt.Reject); err != nil {
		t.Fatal(err)
	}
	if _, err = rtReview(db, s, second.Id, rt.Reject); err != nil {
		t.Fatal(err)
	}
	taskBalance(t, db, a.UserID, accounts.KindUSD, "3")
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "7")
	state, err = s.State(c, a)
	if err != nil || state.PendingUSD != "0.00" {
		t.Fatal(state, err)
	}
	assertLedgerBalanced(t, db)
}
func TestRewardTransferConcurrentRequestsAndReviews(t *testing.T) {
	c := context.Background()
	db, u := userDatabase(t, "reward_transfer_races")
	a := rewardActor(t, u, 113001)
	rtFund(t, db, a.UserID, true, "4")
	mustExec(t, db, "INSERT INTO admin_user(id,email,password_hash,must_change_password) VALUES(1,'review@example.com','test',0)")
	s := rtService(db, transaction.New(db), rtRisk(risk.Review))
	req := rt.Request{Key: "reward_same_request1", Amount: "2"}
	var wg sync.WaitGroup
	rows := make([]*model.RewardTransfer, 6)
	errs := make([]error, 6)
	for i := range rows {
		wg.Add(1)
		go func() { defer wg.Done(); rows[i], errs[i] = s.Submit(c, a, req) }()
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil || rows[i].Id != rows[0].Id {
			t.Fatal(i, e)
		}
	}
	for i := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Submit(c, a, rt.Request{Key: fmt.Sprintf("reward_competing_%02d", i), Amount: "2"})
		}()
	}
	wg.Wait()
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		} else if !errors.Is(e, account.ErrInsufficientBalance) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal(successes)
	}
	for i := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := rt.Approve
			if i%2 == 0 {
				action = rt.Reject
			}
			_, errs[i] = rtReview(db, s, rows[0].Id, action)
		}()
	}
	wg.Wait()
	current, err := s.Detail(c, a, rows[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status == rt.Completed {
		taskBalance(t, db, a.UserID, accounts.KindUSD, "2")
		taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "0")
	} else {
		taskBalance(t, db, a.UserID, accounts.KindUSD, "0")
		taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "2")
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM ledger_voucher WHERE business_type=? AND business_id=?", account.BusinessRewardTransfer, current.Id).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	assertLedgerBalanced(t, db)
}
func TestRewardTransferSchemaAndSubmitRollback(t *testing.T) {
	c := context.Background()
	db, u := userDatabase(t, "reward_transfer_schema")
	a := rewardActor(t, u, 114001)
	rtFund(t, db, a.UserID, true, "5")
	mustExec(t, db, "ALTER TABLE reward_transfer ADD CONSTRAINT test_rt_fault CHECK (submission_voucher_id IS NULL)")
	s := rtService(db, transaction.New(db), rtRisk(risk.Allow))
	if _, err := s.Submit(c, a, rt.Request{Key: "reward_submit_rollback", Amount: "2"}); err == nil {
		t.Fatal("fault ignored")
	}
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "5")
	taskBalance(t, db, a.UserID, accounts.KindUSD, "0")
	if ledgerCount(t, db, "reward_transfer") != 0 {
		t.Fatal("partial application")
	}
	mustExec(t, db, "ALTER TABLE reward_transfer DROP CHECK test_rt_fault")
	name, dsn, full, err := testRun.NewDatabase(c, "reward_transfer_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, f := range []string{"user/tables.sql", "user/constraints.sql", "backoffice/tables.sql", "account/tables.sql", "account/constraints.sql", "rewardtransfer/tables.sql", "rewardtransfer/constraints.sql"} {
		raw, err := os.ReadFile(filepath.Join("../../manifest/sql/ddl", f))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(raw))
	}
	var table, left, right string
	if err = db.QueryRow("SHOW CREATE TABLE reward_transfer").Scan(&table, &left); err != nil {
		t.Fatal(err)
	}
	if err = full.QueryRow("SHOW CREATE TABLE reward_transfer").Scan(&table, &right); err != nil {
		t.Fatal(err)
	}
	autoIncrement := regexp.MustCompile(` AUTO_INCREMENT=\d+`)
	if autoIncrement.ReplaceAllString(left, "") != autoIncrement.ReplaceAllString(right, "") {
		t.Fatalf("DDL differs from migration:\n%s\n%s", left, right)
	}
	_ = name
	_ = dsn
	name, dsn, upgrade, err := testRun.NewDatabase(c, "reward_transfer_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer upgrade.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(13); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	// Reuse the populated application DB to prove down refuses to erase records.
	if _, err = s.Submit(c, a, rt.Request{Key: "reward_submit_after_fault", Amount: "2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../manifest/sql/diff/business/000014_reward_transfer.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err == nil {
		t.Fatal("down erased funds evidence")
	}
}
