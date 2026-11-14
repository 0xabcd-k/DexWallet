//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"sync"
	"testing"
)

func rewardService(db *sql.DB, member tasks.Membership, tx tasks.Transactor) *tasks.Service {
	conn := sqlx.NewSqlConnFromDB(db)
	return tasks.NewWithRewards(conn, member, tx, account.New(accounts.NewUserAccountModel(conn)))
}
func rewardActor(t *testing.T, u *users.Service, tg int64) users.Actor {
	t.Helper()
	v, err := u.ResolveOrCreateVerifiedTelegram(context.Background(), users.VerifiedTelegram{TelegramUserID: tg, FirstName: fmt.Sprintf("user%d", tg)})
	if err != nil {
		t.Fatal(err)
	}
	return users.Actor{UserID: v.User.Id, AuthVersion: v.User.AuthVersion}
}
func rewardRequest(t *testing.T, s *tasks.Service, a users.Actor, taskID, invitationID int64, key string) tasks.ClaimRequest {
	t.Helper()
	p, err := s.Preview(context.Background(), a, taskID, invitationID)
	if err != nil {
		t.Fatal(err)
	}
	return tasks.ClaimRequest{Key: key, TaskID: taskID, InvitationID: invitationID, Mode: p.Mode, Stage: p.Stage, TaskRevision: p.Item.Definition.Revision, IdentityRevision: p.Snapshot.IdentityRevision}
}
func rewardClaim(t *testing.T, s *tasks.Service, a users.Actor, taskID, invitationID int64, key string) *tasks.Completion {
	t.Helper()
	r, err := s.Claim(context.Background(), a, rewardRequest(t, s, a, taskID, invitationID, key))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func alwaysMember(_ context.Context, _ int64, user int64) (tasks.Member, error) {
	return tasks.Member{UserID: user, Status: tasks.MemberRegular}, nil
}
func taskBalance(t *testing.T, db *sql.DB, id int64, kind, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("SELECT COALESCE((SELECT CAST(balance AS CHAR) FROM user_account WHERE user_id=? AND kind=?),'0')", id, kind).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if decimalValue(got) != decimalValue(want) {
		t.Fatalf("user %d %s = %s want %s", id, kind, got, want)
	}
}
func decimalValue(s string) string { return decimal.RequireFromString(s).String() }

func TestTaskRewardsMainPathsAndPro(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_paths")
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	a := rewardActor(t, u, 91001)
	b := rewardActor(t, u, 91002)
	c := rewardActor(t, u, 91003)
	for id := int64(1); id <= 7; id++ {
		taskFixture(t, db, id)
	}
	_, _, err := s.CreateInvitation(ctx, a, 2)
	taskCode(t, err, apperror.TaskInvitationRequiredFirst)
	first := rewardClaim(t, s, a, 1, 0, "reward_first_self_a")
	if len(first.Grants) != 1 || first.Grants[0].Reason != tasks.RewardFirst {
		t.Fatal(first)
	}
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "1")
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "0")
	inv, _, err := s.CreateInvitation(ctx, a, 2)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _, err := s.CreateInvitation(ctx, a, 2)
	if err != nil || duplicate.Id != inv.Id {
		t.Fatal(duplicate, err)
	}
	rewardClaim(t, s, b, 2, inv.Id, "reward_first_help_b")
	taskBalance(t, db, b.UserID, accounts.KindRewardUSD, "1")
	taskBalance(t, db, b.UserID, accounts.KindTaskPoints, "0")
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "100")
	var parent int64
	if err = db.QueryRow("SELECT parent_user_id FROM user_referral WHERE user_id=?", b.UserID).Scan(&parent); err != nil || parent != a.UserID {
		t.Fatal(parent, err)
	}
	// The helper is the inviter's parent: only the pre-existing 20% commission returns to helper.
	invB, _, err := s.CreateInvitation(ctx, b, 3)
	if err != nil {
		t.Fatal(err)
	}
	help := rewardClaim(t, s, a, 3, invB.Id, "reward_old_help_a_b")
	if len(help.Grants) != 1 || help.Grants[0].Reason != tasks.RewardCommission || !help.Grants[0].ProDelta.IsZero() {
		t.Fatal(help)
	}
	taskBalance(t, db, b.UserID, accounts.KindTaskPoints, "100")
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "120")
	// Disabled ancestors retain passive commission, while a disabled inviter cannot receive new help.
	mustExec(t, db, "UPDATE user SET status='disabled' WHERE id=?", a.UserID)
	rewardClaim(t, s, b, 4, 0, "reward_disabled_parent")
	taskBalance(t, db, a.UserID, accounts.KindTaskPoints, "140")
	_, err = s.Preview(ctx, c, 2, inv.Id)
	taskCode(t, err, apperror.TaskInvitationInvalid)
	mustExec(t, db, "UPDATE user SET status='active' WHERE id=?", a.UserID)
	// A has progress 1100 (the commission added none), then reaches 2000 without doubling that award.
	mustExec(t, db, "UPDATE task_definition SET base_points=900,revision=revision+1 WHERE id=5")
	unlocked := rewardClaim(t, s, a, 5, 0, "reward_pro_threshold")
	if len(unlocked.Grants) != 1 || unlocked.Grants[0].UnlockedPro != 1 || unlocked.Grants[0].Points.String() != "900" {
		t.Fatal(unlocked)
	}
	next := rewardClaim(t, s, a, 6, 0, "reward_after_threshold")
	if next.Grants[0].Points.String() != "200" {
		t.Fatal(next)
	}
	invB7, _, err := s.CreateInvitation(ctx, b, 7)
	if err != nil {
		t.Fatal(err)
	}
	rewardClaim(t, s, c, 7, invB7.Id, "reward_three_recipients")
	summary, err := s.Summary(ctx, b)
	if err != nil || summary.Totals.Friends != 1 || summary.Totals.AssistanceCount != 2 {
		t.Fatal(summary, err)
	}
	_, err = s.Completion(ctx, b, first.Row.Id)
	taskCode(t, err, apperror.NotFound)
	_, _, err = s.Grant(ctx, b, first.Grants[0].Id)
	taskCode(t, err, apperror.NotFound)
}

func TestTaskClaimsIdempotencyAndFirstRace(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_race")
	a := rewardActor(t, u, 92001)
	for id := int64(1); id <= 3; id++ {
		taskFixture(t, db, id)
	}
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	r := rewardRequest(t, s, a, 1, 0, "reward_same_request_1")
	var wg sync.WaitGroup
	results := make([]*tasks.Completion, 8)
	errs := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], errs[i] = s.Claim(ctx, a, r) }()
	}
	wg.Wait()
	for i, v := range results {
		if errs[i] != nil || v.Row.Id != results[0].Row.Id {
			t.Fatal(i, errs)
		}
	}
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "1")
	mustExec(t, db, "UPDATE task_definition SET status='paused',revision=revision+1 WHERE id=1")
	replay, err := s.Claim(ctx, a, r)
	if err != nil || replay.Row.Id != results[0].Row.Id {
		t.Fatal(replay, err)
	}
	r.TaskID = 2
	_, err = s.Claim(ctx, a, r)
	taskCode(t, err, apperror.TaskRequestConflict)
	b := rewardActor(t, u, 92002)
	r1 := rewardRequest(t, s, b, 2, 0, "reward_race_first_1")
	r2 := rewardRequest(t, s, b, 3, 0, "reward_race_first_2")
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	racing := rewardService(db, membershipFunc(func(ctx context.Context, ch, user int64) (tasks.Member, error) {
		ready <- struct{}{}
		<-release
		return alwaysMember(ctx, ch, user)
	}), transaction.New(db))
	errs = make([]error, 2)
	for i, r := range []tasks.ClaimRequest{r1, r2} {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = racing.Claim(ctx, b, r) }()
	}
	<-ready
	<-ready
	close(release)
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else {
			taskCode(t, err, apperror.TaskContextChanged)
		}
	}
	if success != 1 {
		t.Fatal(errs)
	}
	taskBalance(t, db, b.UserID, accounts.KindRewardUSD, "1")
}

func TestTaskClaimRollbackAndBindingChange(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_rollback")
	a := rewardActor(t, u, 93001)
	taskFixture(t, db, 1)
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	r := rewardRequest(t, s, a, 1, 0, "reward_atomic_rollback")
	// Fail after completion, grant, voucher, entries and balance changes, at Pro state update.
	mustExec(t, db, "ALTER TABLE user_reward_state ADD CONSTRAINT injected_reward_failure CHECK (pro_progress=0)")
	if _, err := s.Claim(ctx, a, r); err == nil {
		t.Fatal("failure ignored")
	}
	for _, table := range []string{"task_completion", "reward_grant", "ledger_voucher", "ledger_entry", "user_account", "user_reward_state", "user_referral"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	mustExec(t, db, "ALTER TABLE user_reward_state DROP CHECK injected_reward_failure")
	changed := rewardService(db, membershipFunc(func(ctx context.Context, ch, user int64) (tasks.Member, error) {
		mustExec(t, db, "UPDATE telegram_identity SET binding_version=binding_version+2 WHERE user_id=?", a.UserID)
		return alwaysMember(ctx, ch, user)
	}), transaction.New(db))
	_, err := changed.Claim(ctx, a, r)
	taskCode(t, err, apperror.TaskBindingChanged)
	rewardClaim(t, s, a, 1, 0, r.Key)
}

type lostRewardCommit struct{ inner tasks.Transactor }

func (tx lostRewardCommit) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	if err := tx.inner.TransactCtx(ctx, fn); err != nil {
		return err
	}
	return &transaction.Failure{Phase: "commit", Cause: fmt.Errorf("lost commit response")}
}
func TestTaskClaimLostCommitRecovery(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_unknown")
	a := rewardActor(t, u, 94001)
	taskFixture(t, db, 1)
	s := rewardService(db, membershipFunc(alwaysMember), lostRewardCommit{transaction.New(db)})
	r := rewardRequest(t, s, a, 1, 0, "reward_unknown_commit")
	result, err := s.Claim(ctx, a, r)
	if err != nil {
		t.Fatal(err)
	}
	found, err := s.CompletionByRequest(ctx, a, r.Key)
	if err != nil || found.Row.Id != result.Row.Id {
		t.Fatal(found, err)
	}
	taskBalance(t, db, a.UserID, accounts.KindRewardUSD, "1")
}

func TestTaskHelpRollsBackAllRecipientsAndRelation(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_help_rollback")
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	parent := rewardActor(t, u, 95001)
	inviter := rewardActor(t, u, 95002)
	child := rewardActor(t, u, 95003)
	for id := int64(1); id <= 3; id++ {
		taskFixture(t, db, id)
	}
	rewardClaim(t, s, parent, 1, 0, "setup_first_parent")
	parentInvite, _, err := s.CreateInvitation(ctx, parent, 2)
	if err != nil {
		t.Fatal(err)
	}
	rewardClaim(t, s, inviter, 2, parentInvite.Id, "setup_first_inviter")
	invite, _, err := s.CreateInvitation(ctx, inviter, 3)
	if err != nil {
		t.Fatal(err)
	}
	// All three grants and the new relationship are written before this constraint fails.
	mustExec(t, db, fmt.Sprintf("ALTER TABLE user_reward_state ADD CONSTRAINT injected_child_progress CHECK(user_id<>%d OR pro_progress=0)", child.UserID))
	r := rewardRequest(t, s, child, 3, invite.Id, "help_atomic_rollback")
	if _, err = s.Claim(ctx, child, r); err == nil {
		t.Fatal("ignored child progress failure")
	}
	var completedCount, relationCount int
	if err = db.QueryRow("SELECT COUNT(*) FROM task_completion WHERE user_id=?", child.UserID).Scan(&completedCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM user_referral WHERE user_id=?", child.UserID).Scan(&relationCount); err != nil {
		t.Fatal(err)
	}
	if completedCount != 0 || relationCount != 0 {
		t.Fatal("partial completion or relationship", completedCount, relationCount)
	}
	taskBalance(t, db, child.UserID, accounts.KindRewardUSD, "0")
	taskBalance(t, db, inviter.UserID, accounts.KindTaskPoints, "0")
	taskBalance(t, db, parent.UserID, accounts.KindTaskPoints, "100")
	mustExec(t, db, "ALTER TABLE user_reward_state DROP CHECK injected_child_progress")
	if _, err = s.Claim(ctx, child, r); err != nil {
		t.Fatal(err)
	}
	taskBalance(t, db, child.UserID, accounts.KindRewardUSD, "1")
	taskBalance(t, db, inviter.UserID, accounts.KindTaskPoints, "100")
	taskBalance(t, db, parent.UserID, accounts.KindTaskPoints, "120")
}

func TestTaskUnknownMissAndIdentityConsumption(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_unknown_miss")
	a := rewardActor(t, u, 96001)
	taskFixture(t, db, 1)
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	r := rewardRequest(t, s, a, 1, 0, "unknown_without_receipt")
	unknown := rewardService(db, membershipFunc(alwaysMember), transferTx(func(context.Context, func(context.Context, sqlx.Session) error) error {
		return &transaction.Failure{Phase: "commit", Cause: fmt.Errorf("unknown transport result")}
	}))
	if _, err := unknown.Claim(ctx, a, r); !errors.Is(err, tasks.ErrClaimUnknown) {
		t.Fatal(err)
	}
	_, err := s.CompletionByRequest(ctx, a, r.Key)
	taskCode(t, err, apperror.NotFound)
	first, err := s.Claim(ctx, a, r)
	if err != nil {
		t.Fatal(err)
	}
	other, err := u.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "claim_other@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE telegram_identity SET user_id=?,binding_version=binding_version+1 WHERE user_id=?", other.User.Id, a.UserID)
	b := users.Actor{UserID: other.User.Id, AuthVersion: other.User.AuthVersion}
	_, err = s.Claim(ctx, b, rewardRequest(t, s, b, 1, 0, "used_identity_new_user"))
	taskCode(t, err, apperror.TaskAlreadyCompleted)
	_, err = s.Completion(ctx, b, first.Row.Id)
	taskCode(t, err, apperror.NotFound)
	// Original owner can still recover the historical receipt without the old TG binding.
	if _, err = s.Claim(ctx, a, r); err != nil {
		t.Fatal(err)
	}
}

func TestTaskConcurrentAssistanceUsesCurrentPro(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "reward_parallel_pro")
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	inviter := rewardActor(t, u, 97001)
	a := rewardActor(t, u, 97002)
	b := rewardActor(t, u, 97003)
	for id := int64(1); id <= 4; id++ {
		taskFixture(t, db, id)
	}
	mustExec(t, db, "UPDATE task_definition SET base_points=500 WHERE id>=2")
	rewardClaim(t, s, inviter, 1, 0, "pro_setup_first_task")
	rewardClaim(t, s, inviter, 2, 0, "pro_setup_1500_points")
	inviteA, _, err := s.CreateInvitation(ctx, inviter, 3)
	if err != nil {
		t.Fatal(err)
	}
	inviteB, _, err := s.CreateInvitation(ctx, inviter, 4)
	if err != nil {
		t.Fatal(err)
	}
	ra := rewardRequest(t, s, a, 3, inviteA.Id, "pro_concurrent_help_a")
	rb := rewardRequest(t, s, b, 4, inviteB.Id, "pro_concurrent_help_b")
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	parallel := rewardService(db, membershipFunc(func(ctx context.Context, ch, id int64) (tasks.Member, error) {
		ready <- struct{}{}
		<-release
		return alwaysMember(ctx, ch, id)
	}), transaction.New(db))
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, r := range []tasks.ClaimRequest{ra, rb} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := a
			if i == 1 {
				actor = b
			}
			_, errs[i] = parallel.Claim(ctx, actor, r)
		}()
	}
	<-ready
	<-ready
	close(release)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	taskBalance(t, db, inviter.UserID, accounts.KindTaskPoints, "2000")
	summary, err := s.Summary(ctx, inviter)
	if err != nil || summary.State.Progress.String() != "3000" || !summary.State.UnlockedAt.Valid {
		t.Fatal(summary, err)
	}
	var unlocked int
	if err = db.QueryRow("SELECT SUM(unlocked_pro) FROM reward_grant WHERE user_id=?", inviter.UserID).Scan(&unlocked); err != nil || unlocked != 1 {
		t.Fatal(unlocked, err)
	}
}
