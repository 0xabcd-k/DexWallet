//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/draw"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func drawService(db *sql.DB, tx draw.Transactor, enabled bool) *draw.Service {
	conn := sqlx.NewSqlConnFromDB(db)
	return draw.New(conn, tx, account.New(accounts.NewUserAccountModel(conn)), enabled)
}
func drawFund(t *testing.T, db *sql.DB, userID, points int64) {
	t.Helper()
	tx := transaction.New(db)
	a := account.New(accounts.NewUserAccountModel(sqlx.NewSqlConnFromDB(db)))
	err := tx.TransactCtx(context.Background(), func(ctx context.Context, session sqlx.Session) error {
		_, err := a.Post(ctx, session, account.Posting{Source: account.Source{Type: account.BusinessReward, ID: 900000 + userID, Action: account.ActionPost}, Entries: []account.Entry{
			account.Credit(userID, accounts.KindTaskPoints, decimal.NewFromInt(points)), {System: account.SystemPointIssuance, Side: account.SideDebit, Amount: decimal.NewFromInt(points)},
		}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
func publishDraw(t *testing.T, s *draw.Service, name string) int64 {
	t.Helper()
	v, err := s.Publish(context.Background(), name, draw.SampleRules())
	if err != nil {
		t.Fatal(err)
	}
	return v.Id
}
func TestDrawAtomicSettlementAndHistory(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "draw_paths")
	a := rewardActor(t, u, 101001)
	other := rewardActor(t, u, 101002)
	s := drawService(db, transaction.New(db), true)
	initial, err := s.State(ctx, a)
	if err != nil || initial.Version != nil || initial.Points != "0" {
		t.Fatal(initial, err)
	}
	v := publishDraw(t, s, "candidate_v1")
	drawFund(t, db, a.UserID, 200)
	mustExec(t, db, "INSERT INTO user_reward_state(user_id,pro_progress,pro_unlocked_at) VALUES(?,2000,UTC_TIMESTAMP(6))", a.UserID)
	r := draw.Request{Key: "draw_atomic_request1", VersionID: v}
	first, err := s.Draw(ctx, a, r)
	if err != nil {
		t.Fatal(err)
	}
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "100")
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, first.Row.RewardUsd.String())
	var progress string
	if err = db.QueryRow("SELECT pro_progress FROM user_reward_state WHERE user_id=?", a.UserID).Scan(&progress); err != nil || decimalValue(progress) != "2000" {
		t.Fatal(progress, err)
	}
	assertLedgerBalanced(t, db)
	if ledgerCount(t, db, "ledger_voucher") != 2 {
		t.Fatal("expected funding and one balanced draw voucher")
	}
	mustExec(t, db, "UPDATE draw_control SET version_id=NULL")
	again, err := s.Draw(ctx, a, r)
	if err != nil || again.Row.Id != first.Row.Id || again.Row.Symbols != first.Row.Symbols {
		t.Fatal(again, err)
	}
	r.Key = "draw_paused_request"
	_, err = s.Draw(ctx, a, r)
	taskCode(t, err, apperror.DrawUnavailable)
	v2 := publishDraw(t, s, "candidate_v2")
	_, err = s.Draw(ctx, a, r)
	taskCode(t, err, apperror.DrawVersionChanged)
	r.Key = "draw_atomic_request1"
	r.VersionID = v2
	_, err = s.Draw(ctx, a, r)
	taskCode(t, err, apperror.DrawRequestConflict)
	_, err = s.Detail(ctx, other, first.Row.Id)
	taskCode(t, err, apperror.NotFound)
	second, err := s.Draw(ctx, a, draw.Request{Key: "draw_atomic_request2", VersionID: v2})
	if err != nil {
		t.Fatal(err)
	}
	rows, next, err := s.List(ctx, a, 0, 1)
	if err != nil || len(rows) != 1 || rows[0].Row.Id != second.Row.Id || next != second.Row.Id {
		t.Fatal(rows, next, err)
	}
	rows, _, err = s.List(ctx, a, next, 1)
	if err != nil || len(rows) != 1 || rows[0].VersionName != "candidate_v1" {
		t.Fatal(rows, err)
	}
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "0")
	_, err = s.Draw(ctx, a, draw.Request{Key: "draw_insufficient_1", VersionID: v2})
	if !errors.Is(err, account.ErrInsufficientBalance) {
		t.Fatal(err)
	}
	if ledgerCount(t, db, "point_draw") != 2 || ledgerCount(t, db, "ledger_voucher") != 3 {
		t.Fatal("insufficient balance left effects")
	}
	disabled := drawService(db, transaction.New(db), false)
	state, err := disabled.State(ctx, a)
	if err != nil || state.Version != nil {
		t.Fatal(state, err)
	}
	if _, err = disabled.ByRequest(ctx, a, "draw_atomic_request1"); err != nil {
		t.Fatal(err)
	}
	_, err = disabled.Draw(ctx, a, draw.Request{Key: "draw_disabled_new_1", VersionID: v2})
	taskCode(t, err, apperror.DrawUnavailable)
	mustExec(t, db, "UPDATE user SET auth_version=auth_version+1 WHERE id=?", a.UserID)
	_, err = s.Draw(ctx, a, draw.Request{Key: "draw_stale_session1", VersionID: v2})
	if !errors.Is(err, users.ErrStaleSession) {
		t.Fatal(err)
	}
}
func TestDrawConcurrencyAndNoOverspend(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "draw_race")
	a := rewardActor(t, u, 102001)
	b := rewardActor(t, u, 102002)
	s := drawService(db, transaction.New(db), true)
	v := publishDraw(t, s, "candidate")
	drawFund(t, db, a.UserID, 100)
	drawFund(t, db, b.UserID, 100)
	var wg sync.WaitGroup
	results := make([]*draw.Result, 8)
	errs := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.Draw(ctx, a, draw.Request{Key: "draw_parallel_same1", VersionID: v})
		}()
	}
	wg.Wait()
	for i, r := range results {
		if errs[i] != nil || r.Row.Id != results[0].Row.Id {
			t.Fatal(i, errs)
		}
	}
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.Draw(ctx, b, draw.Request{Key: fmt.Sprintf("draw_parallel_%04d", i), VersionID: v})
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, account.ErrInsufficientBalance) {
			t.Fatal(err)
		}
	}
	if successes != 1 || ledgerCount(t, db, "point_draw") != 2 {
		t.Fatal(successes)
	}
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "0")
	taskBalance(t, db, b.UserID, accounts.KindTaskPoints, "0")
	assertLedgerBalanced(t, db)
}
func TestDrawRollbackAndUnknownCommit(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "draw_rollback")
	a := rewardActor(t, u, 103001)
	s := drawService(db, transaction.New(db), true)
	v := publishDraw(t, s, "candidate")
	drawFund(t, db, a.UserID, 100)
	mustExec(t, db, "ALTER TABLE point_draw ADD CONSTRAINT test_draw_fault CHECK (voucher_id IS NULL)")
	request := draw.Request{Key: "draw_rollback_request", VersionID: v}
	if _, err := s.Draw(ctx, a, request); err == nil {
		t.Fatal("fault did not fail")
	}
	if ledgerCount(t, db, "point_draw") != 0 || ledgerCount(t, db, "ledger_voucher") != 1 {
		t.Fatal("partial draw survived")
	}
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "100")
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "0")
	mustExec(t, db, "ALTER TABLE point_draw DROP CHECK test_draw_fault")
	unknown := drawService(db, lostRewardCommit{transaction.New(db)}, true)
	result, err := unknown.Draw(ctx, a, request)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.Draw(ctx, a, request)
	if err != nil || repeated.Row.Id != result.Row.Id {
		t.Fatal(repeated, err)
	}
	missing := drawService(db, transferTx(func(context.Context, func(context.Context, sqlx.Session) error) error {
		return &transaction.Failure{Phase: "commit", Cause: errors.New("unknown")}
	}), true)
	_, err = missing.Draw(ctx, a, draw.Request{Key: "draw_unknown_no_record", VersionID: v})
	if !errors.Is(err, draw.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
}
func TestDrawVersionValidationAndPublication(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "draw_versions")
	a := rewardActor(t, u, 104001)
	s := drawService(db, transaction.New(db), true)
	v := publishDraw(t, s, "candidate")
	if id := publishDraw(t, s, "candidate"); id != v {
		t.Fatal("duplicate version")
	}
	r := draw.SampleRules()
	r.Prizes[0].Weight++
	if _, err := s.Publish(ctx, "invalid", r); err == nil {
		t.Fatal("published invalid weights")
	}
	r = draw.SampleRules()
	r.Prizes[0].Weight = 6500
	r.Prizes[1].Weight = 0
	r.RTP = decimal.New(90, -2)
	if _, err := s.Publish(ctx, "candidate", r); err == nil {
		t.Fatal("overwrote immutable version")
	}
	if ledgerCount(t, db, "draw_rule_version") != 1 {
		t.Fatal("invalid versions persisted")
	}
	state, err := s.State(ctx, a)
	if err != nil || state.Version.Row.Id != v {
		t.Fatal(state, err)
	}
	// A draw holds a shared pointer lock until its settlement completes; publication
	// may wait but cannot alter the version used by an in-flight draw.
	locked := make(chan struct{})
	release := make(chan struct{})
	tx := transferTx(func(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
		return transaction.New(db).TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			if err := fn(ctx, session); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	})
	drawFund(t, db, a.UserID, 100)
	running := drawService(db, tx, true)
	drawDone := make(chan error, 1)
	go func() {
		_, err := running.Draw(ctx, a, draw.Request{Key: "draw_version_inflight", VersionID: v})
		drawDone <- err
	}()
	<-locked
	publishDone := make(chan error, 1)
	go func() { _, err := s.Publish(ctx, "v2", r); publishDone <- err }()
	close(release)
	if err := <-drawDone; err != nil {
		t.Fatal(err)
	}
	if err := <-publishDone; err != nil {
		t.Fatal(err)
	}
	result, err := s.ByRequest(ctx, a, "draw_version_inflight")
	if err != nil || result.Row.VersionId != v {
		t.Fatal(result, err)
	}
}
func TestDrawSchemaMigrationAndIsolatedTool(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "draw_schema")
	_, _, full, err := testRun.NewDatabase(ctx, "draw_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, file := range []string{"user/tables.sql", "user/constraints.sql", "account/tables.sql", "account/constraints.sql", "draw/tables.sql", "draw/constraints.sql"} {
		ddl, err := os.ReadFile(filepath.Join("../../manifest/sql/ddl", file))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	for _, name := range []string{"draw_rule_version", "draw_control", "point_draw"} {
		var table, a, b string
		if err = db.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &a); err != nil {
			t.Fatal(err)
		}
		if err = full.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &b); err != nil || a != b {
			t.Fatal(name, a, b, err)
		}
	}
	name, dsn, upgrade, err := testRun.NewDatabase(ctx, "draw_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer upgrade.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(12); err != nil {
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
	cmd := exec.Command("go", "run", "./tools/drawdemo", "--database", name)
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn, "DEXWALLET_ID_SECRET=test-only-key123")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "candidate_v1") {
		t.Fatalf("%v %s", err, output)
	}
	if ledgerCount(t, upgrade, "user_account") != 0 || ledgerCount(t, upgrade, "point_draw") != 0 {
		t.Fatal("tool created user money")
	}
	if err = m.Migrate(12); err == nil {
		t.Fatal("dropped published configuration")
	}
}

func TestDrawConcurrentTaskRewardKeepsProgress(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "draw_task")
	a := rewardActor(t, u, 105001)
	tasks := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	taskFixture(t, db, 1)
	taskFixture(t, db, 2)
	rewardClaim(t, tasks, a, 1, 0, "draw_task_firstclaim")
	drawFund(t, db, a.UserID, 100)
	s := drawService(db, transaction.New(db), true)
	v := publishDraw(t, s, "candidate")
	request := rewardRequest(t, tasks, a, 2, 0, "draw_task_returning1")
	errs := make(chan error, 2)
	go func() { _, err := s.Draw(ctx, a, draw.Request{Key: "draw_task_concurrent", VersionID: v}); errs <- err }()
	go func() { _, err := tasks.Claim(ctx, a, request); errs <- err }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "100")
	summary, err := tasks.Summary(ctx, a)
	if err != nil || summary.State.Progress.String() != "1100" {
		t.Fatal(summary, err)
	}
	assertLedgerBalanced(t, db)
}
