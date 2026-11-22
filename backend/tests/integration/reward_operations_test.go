//go:build integration

package integration

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"testing"
	"time"
)

func TestInvitationResultsPrivacyAndAttribution(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "invitation_results")
	s := rewardService(db, membershipFunc(alwaysMember), transaction.New(db))
	parent := rewardActor(t, u, 98101)
	child := rewardActor(t, u, 98102)
	helper := rewardActor(t, u, 98103)
	for id := int64(1); id <= 5; id++ {
		taskFixture(t, db, id)
	}
	rewardClaim(t, s, parent, 1, 0, "results_parent_first")
	inv, _, err := s.CreateInvitation(ctx, parent, 2)
	if err != nil {
		t.Fatal(err)
	}
	rewardClaim(t, s, child, 2, inv.Id, "results_child_first")
	childInv, _, err := s.CreateInvitation(ctx, child, 3)
	if err != nil {
		t.Fatal(err)
	}
	rewardClaim(t, s, helper, 3, childInv.Id, "results_helper_first")
	rewardClaim(t, s, child, 4, 0, "results_child_returning")
	friends, next, err := s.Friends(ctx, parent, 0, 1)
	if err != nil || next != 0 || len(friends) != 1 || friends[0].UserID != child.UserID {
		t.Fatal(friends, next, err)
	}
	summary, err := s.Summary(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	totals := summary.Totals
	if totals.Friends != 1 || totals.AssistanceCount != 1 || totals.AssistancePoints.String() != "100" || totals.CommissionPoints.String() != "40" {
		t.Fatalf("parent lifetime totals across three grants: %+v", totals)
	}
	other, err := s.Summary(ctx, helper)
	if err != nil {
		t.Fatal(err)
	}
	if other.Totals.Friends != 0 || other.Totals.AssistanceCount != 0 || !other.Totals.AssistancePoints.IsZero() || !other.Totals.CommissionPoints.IsZero() {
		t.Fatalf("helper without invitation income: %+v", other.Totals)
	}
	filter := tasks.EarningsFilter{Limit: 1, Reason: tasks.EarningsAll}
	rows, next, err := s.Earnings(ctx, parent, filter)
	if err != nil || next == 0 || len(rows) != 1 || rows[0].PeerID != child.UserID || rows[0].Reason != tasks.RewardCommission {
		t.Fatal(rows, next, err)
	}
	filter.Before = next
	rows, next, err = s.Earnings(ctx, parent, filter)
	if err != nil || next == 0 || len(rows) != 1 || rows[0].PeerID != child.UserID {
		t.Fatal("help-mode commission must name child, not helper", rows, err)
	}
	filter.Before = next
	rows, next, err = s.Earnings(ctx, parent, filter)
	if err != nil || next != 0 || len(rows) != 1 || rows[0].Reason != tasks.RewardAssistance {
		t.Fatal(rows, next, err)
	}
	_, _, err = s.Grant(ctx, helper, rows[0].ID)
	taskCode(t, err, apperror.NotFound)
	filter = tasks.EarningsFilter{Limit: 20, Reason: tasks.EarningsAll, FriendID: helper.UserID}
	rows, _, err = s.Earnings(ctx, parent, filter)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	filter.FriendID = child.UserID
	filter.Reason = tasks.RewardCommission
	filter.From = time.Now().UTC().Format(time.DateOnly)
	filter.To = filter.From
	rows, _, err = s.Earnings(ctx, parent, filter)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	filter.To = "2000-01-01"
	_, _, err = s.Earnings(ctx, parent, filter)
	taskCode(t, err, apperror.InvalidArgument)
	rows, _, err = s.Earnings(ctx, helper, tasks.EarningsFilter{Limit: 20, Reason: tasks.EarningsAll})
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestRewardOperationsMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "ops_migration")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(14); err != nil {
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
	tx := transaction.New(db)
	member, _ := tasks.NewMock(nil)
	s := rewardService(db, member, tx)
	d := tasks.Draft{CreatorID: 1, RequestKey: "migration_task_draft", Title: "sample", BasePoints: 100, ChannelID: -100, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour)}
	err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		_, _, err := s.SaveDraftInSession(ctx, session, d)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(14); err == nil {
		t.Fatal("discarded used creation keys")
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM task_definition WHERE create_key IS NOT NULL").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
