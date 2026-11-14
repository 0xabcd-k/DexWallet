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
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/task"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type membershipFunc func(context.Context, int64, int64) (tasks.Member, error)

func (f membershipFunc) GetMember(ctx context.Context, channel, user int64) (tasks.Member, error) {
	return f(ctx, channel, user)
}
func taskFixture(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	mustExec(t, db, "INSERT INTO task_definition(id,kind,status,verification_mode,title,description,base_points,channel_id,starts_at,ends_at) VALUES(?,'telegram_channel_follow','active','mock','Sample channel','No real subscription',100,-100,DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 HOUR),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR))", id)
}
func taskCode(t *testing.T, err error, want apperror.Code) {
	t.Helper()
	var app *apperror.Error
	if !errors.As(err, &app) || app.Code != want {
		t.Fatalf("want %s got %v", want, err)
	}
}
func TestTaskEligibilityAndReadOnlyChecks(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "task_checks")
	taskFixture(t, db, 1)
	email, err := u.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "task@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	actor := users.Actor{UserID: email.User.Id, AuthVersion: email.User.AuthVersion}
	calls := 0
	s := tasks.New(sqlx.NewSqlConnFromDB(db), membershipFunc(func(_ context.Context, channel, user int64) (tasks.Member, error) {
		calls++
		if channel != -100 || user != 701 {
			t.Fatal("untrusted identity", channel, user)
		}
		return tasks.Member{UserID: user, Status: tasks.MemberRegular}, nil
	}))
	detail, err := s.Detail(ctx, actor, 1)
	if err != nil || detail.Eligibility != tasks.TelegramRequired {
		t.Fatal(detail, err)
	}
	_, err = s.Check(ctx, actor, 1)
	taskCode(t, err, apperror.TaskTelegramRequired)
	if calls != 0 {
		t.Fatal("checked unbound user")
	}
	linked, err := u.ChangeIdentity(ctx, actor, users.Bind, "telegram", users.EmailAddress{}, &users.VerifiedTelegram{TelegramUserID: 701, FirstName: "Sample"})
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = linked.User.AuthVersion
	for range 2 {
		result, err := s.Check(ctx, actor, 1)
		if err != nil || result.Outcome != tasks.OutcomeMember || result.CheckedAt.IsZero() {
			t.Fatal(result, err)
		}
	}
	var n int
	for _, table := range []string{"task_completion", "ledger_voucher", "user_account"} {
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("preview wrote", table, n, err)
		}
	}
	for _, state := range []struct {
		sql  string
		want string
	}{{"status='paused'", tasks.Paused}, {"status='active',starts_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL 30 MINUTE)", tasks.NotStarted}, {"starts_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 2 HOUR),ends_at=UTC_TIMESTAMP(6)", tasks.Expired}} {
		mustExec(t, db, "UPDATE task_definition SET "+state.sql+" WHERE id=1")
		d, err := s.Detail(ctx, actor, 1)
		if err != nil || d.Eligibility != state.want {
			t.Fatal(d, err)
		}
		_, err = s.Check(ctx, actor, 1)
		taskCode(t, err, apperror.TaskUnavailable)
	}
	mustExec(t, db, "UPDATE task_definition SET starts_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 HOUR),ends_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL 1 HOUR) WHERE id=1")
	for _, fixture := range []tasks.Fixture{{ChannelID: -100, TelegramUserID: 701, Status: tasks.MemberLeft}, {ChannelID: -100, TelegramUserID: 701, Failure: tasks.FailurePermission}, {ChannelID: -100, TelegramUserID: 701, Failure: tasks.FailureTimeout}, {ChannelID: -100, TelegramUserID: 701, Failure: tasks.FailureRateLimited}, {ChannelID: -100, TelegramUserID: 701, Failure: tasks.FailureInvalidResponse}} {
		m, _ := tasks.NewMock([]tasks.Fixture{fixture})
		check := tasks.New(sqlx.NewSqlConnFromDB(db), m)
		result, err := check.Check(ctx, actor, 1)
		if fixture.Failure != "" {
			taskCode(t, err, apperror.TaskVerificationUnavailable)
		} else if err != nil || result.Outcome != tasks.OutcomeNotMember {
			t.Fatal(result, err)
		}
	}
	// Editing the task during an external call cannot validate an obsolete target.
	changed := tasks.New(sqlx.NewSqlConnFromDB(db), membershipFunc(func(_ context.Context, _, user int64) (tasks.Member, error) {
		mustExec(t, db, "UPDATE task_definition SET channel_id=-101,revision=revision+1 WHERE id=1")
		return tasks.Member{UserID: user, Status: tasks.MemberRegular}, nil
	}))
	_, err = changed.Check(ctx, actor, 1)
	taskCode(t, err, apperror.TaskUnavailable)
	// Unbind/rebind even the same identity invalidates the snapshot.
	changed = tasks.New(sqlx.NewSqlConnFromDB(db), membershipFunc(func(_ context.Context, _, user int64) (tasks.Member, error) {
		mustExec(t, db, "UPDATE telegram_identity SET binding_version=binding_version+2 WHERE telegram_user_id=?", user)
		return tasks.Member{UserID: user, Status: tasks.MemberRegular}, nil
	}))
	_, err = changed.Check(ctx, actor, 1)
	taskCode(t, err, apperror.TaskBindingChanged)
	for _, update := range []struct {
		sql  string
		want error
	}{{"auth_version=auth_version+1", users.ErrStaleSession}, {"status='disabled'", users.ErrDisabled}} {
		mustExec(t, db, "UPDATE user SET "+update.sql+" WHERE id=?", actor.UserID)
		_, err = s.Check(ctx, actor, 1)
		if !errors.Is(err, update.want) {
			t.Fatal(err)
		}
	}
}
func TestTaskCatalogAndIdentityConsumption(t *testing.T) {
	ctx := context.Background()
	db, u := userDatabase(t, "task_identity")
	account, err := u.ResolveOrCreateVerifiedTelegram(ctx, users.VerifiedTelegram{TelegramUserID: 702, FirstName: "One"})
	if err != nil {
		t.Fatal(err)
	}
	actor := users.Actor{UserID: account.User.Id, AuthVersion: account.User.AuthVersion}
	for _, id := range []int64{1, 2, 3, 4} {
		taskFixture(t, db, id)
	}
	mustExec(t, db, "UPDATE task_definition SET status='draft' WHERE id=4")
	mock, _ := tasks.NewMock([]tasks.Fixture{{ChannelID: -100, TelegramUserID: 702, Status: tasks.MemberRegular}})
	conn := sqlx.NewSqlConnFromDB(db)
	s := tasks.New(conn, mock)
	first, err := s.List(ctx, actor, 0, 2)
	if err != nil || len(first.Items) != 2 || first.NextID != 2 {
		t.Fatal(first, err)
	}
	next, err := s.List(ctx, actor, first.NextID, 2)
	if err != nil || len(next.Items) != 1 || next.Items[0].Definition.Id != 1 || next.NextID != 0 {
		t.Fatal(next, err)
	}
	_, err = s.Detail(ctx, actor, 4)
	taskCode(t, err, apperror.NotFound)
	disabled := tasks.New(conn, nil)
	empty, err := disabled.List(ctx, actor, 0, 20)
	if err != nil || len(empty.Items) != 0 {
		t.Fatal(empty, err)
	}
	_, err = disabled.Check(ctx, actor, 1)
	taskCode(t, err, apperror.NotFound)
	// Fixture represents a past atomically settled completion; preview cannot create it.
	completion := &model.TaskCompletion{TaskId: 1, TelegramIdentityId: account.Binding.IdentityID, UserId: actor.UserID, RequestKey: "past_task_request_0", TaskRevision: 1, BasePoints: 100, VerifiedAt: time.Now().UTC()}
	completions := model.NewTaskCompletionModel(conn)
	tx := transaction.New(db)
	rollback := errors.New("rollback")
	err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		if _, err := completions.WithSession(session).Insert(ctx, completion); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, actor, 1)
	if err != nil || detail.Eligibility != tasks.Eligible {
		t.Fatal("rollback consumed eligibility", detail, err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row := *completion
			row.RequestKey = fmt.Sprintf("concurrent_task_%d", i)
			_, errs[i] = completions.Insert(ctx, &row)
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("duplicate completion", errs)
	}
	_, err = s.Check(ctx, actor, 1)
	taskCode(t, err, apperror.TaskAlreadyCompleted)
	other, err := u.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "other_task@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	// Reassignment fixture emulates the final binding state without deleting identity history.
	mustExec(t, db, "UPDATE telegram_identity SET user_id=?,binding_version=binding_version+1 WHERE id=?", other.User.Id, account.Binding.IdentityID)
	otherActor := users.Actor{UserID: other.User.Id, AuthVersion: other.User.AuthVersion}
	_, err = s.Check(ctx, otherActor, 1)
	taskCode(t, err, apperror.TaskAlreadyCompleted)
	result, err := s.Check(ctx, otherActor, 2)
	if err != nil || result.Outcome != tasks.OutcomeMember {
		t.Fatal("another task incorrectly blocked", result, err)
	}
	var beneficiary int64
	if err = db.QueryRow("SELECT user_id FROM task_completion WHERE task_id=1").Scan(&beneficiary); err != nil || beneficiary != actor.UserID {
		t.Fatal("moved old completion", beneficiary, err)
	}
}
func TestTaskSchemaAndMigration(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "task_schema")
	_, _, full, err := testRun.NewDatabase(ctx, "task_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, name := range []string{"user/tables.sql", "user/constraints.sql", "account/tables.sql", "account/constraints.sql", "transfer/tables.sql", "transfer/constraints.sql", "task/tables.sql", "task/constraints.sql"} {
		ddl, err := os.ReadFile(filepath.Join("../../manifest/sql/ddl", name))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	for _, name := range []string{"task_definition", "task_completion"} {
		var table, a, b string
		if err = db.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &a); err != nil {
			t.Fatal(err)
		}
		if err = full.QueryRow("SHOW CREATE TABLE "+name).Scan(&table, &b); err != nil || a != b {
			t.Fatal("schema mismatch", name, a, b, err)
		}
	}
	// Empty migration reverses, but configured tasks must not be silently dropped.
	name, dsn, upgrade, err := testRun.NewDatabase(ctx, "task_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer upgrade.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(10); err != nil {
		t.Fatal(err)
	}
	mustExec(t, upgrade, "INSERT INTO user(id,nickname) VALUES(77,'existing')")
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./tools/taskdemo", "--database", name)
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn, "DEXWALLET_ID_SECRET=test-only-key123")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Mode: mock") || !strings.Contains(string(output), "/#/tasks/") {
		t.Fatalf("sample tool: %v %s", err, output)
	}
	if err = m.Steps(-1); err == nil {
		t.Fatal("discarded task configuration")
	}
	var count int
	if err = upgrade.QueryRow("SELECT COUNT(*) FROM user WHERE id=77").Scan(&count); err != nil || count != 1 {
		t.Fatal("lost old user", err)
	}
}
