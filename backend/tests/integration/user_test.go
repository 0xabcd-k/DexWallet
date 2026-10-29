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

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	driver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func userDatabase(t *testing.T, label string) (*sql.DB, *userlogic.Service) {
	t.Helper()
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, label)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		a, b := m.Close()
		if a != nil || b != nil {
			t.Errorf("migration close: %v %v", a, b)
		}
	}()
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	if err = m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("repeat up: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != 2 || dirty {
		t.Fatalf("version: %d %t %v", version, dirty, err)
	}
	conn := sqlx.NewSqlConnFromDB(db)
	return db, userlogic.New(model.NewUserModel(conn), model.NewEmailIdentityModel(conn), model.NewTelegramIdentityModel(conn), transaction.New(db))
}
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}
func TestUserSchemaAndConstraints(t *testing.T) {
	db, _ := userDatabase(t, "user_schema")
	_, _, full, err := testRun.NewDatabase(context.Background(), "user_full")
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	for _, name := range []string{"tables.sql", "constraints.sql"} {
		ddl, err := os.ReadFile(filepath.Join("../../manifest/sql/ddl/user", name))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, full, string(ddl))
	}
	for _, name := range []string{"user", "email_identity", "telegram_identity"} {
		var table, a, b string
		if err := db.QueryRow("SHOW CREATE TABLE `"+name+"`").Scan(&table, &a); err != nil {
			t.Fatal(err)
		}
		if err := full.QueryRow("SHOW CREATE TABLE `"+name+"`").Scan(&table, &b); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("schema differs for %s:\n%s\n%s", name, a, b)
		}
		if strings.Contains(a, "`idx`") {
			t.Fatal("idx persisted")
		}
	}
	mustExec(t, db, "INSERT INTO `user`(id) VALUES(1),(2)")
	mustExec(t, db, "INSERT INTO email_identity(email_address,email_local_part,email_domain,email_original,user_id) VALUES('A@example.com','A','example.com','A@example.com',1),('a@example.com','a','example.com','a@example.com',NULL),('b@example.com','b','example.com','b@example.com',NULL)")
	mustExec(t, db, "INSERT INTO telegram_identity(telegram_user_id,first_name,user_id) VALUES(1,'one',1),(2,'two',NULL),(3,'three',NULL)")
	for _, tc := range []struct {
		query string
		code  uint16
	}{
		{"INSERT INTO email_identity(email_address,email_local_part,email_domain,email_original) VALUES('A@example.com','A','example.com','A@example.com')", 1062},
		{"UPDATE email_identity SET user_id=1 WHERE email_address='b@example.com'", 1062},
		{"UPDATE email_identity SET user_id=999 WHERE email_address='b@example.com'", 1452},
		{"INSERT INTO telegram_identity(telegram_user_id,first_name) VALUES(1,'duplicate')", 1062},
		{"UPDATE telegram_identity SET user_id=1 WHERE telegram_user_id=2", 1062},
		{"UPDATE telegram_identity SET user_id=999 WHERE telegram_user_id=2", 1452},
		{"DELETE FROM `user` WHERE id=1", 1451},
	} {
		_, err := db.Exec(tc.query)
		var e *driver.MySQLError
		if !errors.As(err, &e) || e.Number != tc.code {
			t.Fatalf("constraint %s: %v", tc.query, err)
		}
	}
	// Business and example histories can coexist without stealing each other's versions.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations_business WHERE version=2 AND dirty=0").Scan(&count); err != nil || count != 1 {
		t.Fatalf("business history: %d %v", count, err)
	}
	t.Log("full DDL equals migration including foreign keys; unique ownership, NULL owners and case-sensitive addresses verified")
}
func TestUserIdentityLifecycle(t *testing.T) {
	db, s := userDatabase(t, "user_lifecycle")
	ctx := context.Background()
	first, err := s.ResolveOrCreateVerifiedEmail(ctx, " Alice.Tag+one@EXAMPLE.COM ")
	if err != nil {
		t.Fatal(err)
	}
	identities, err := s.FindCurrentIdentities(ctx, first.User.Id)
	if err != nil || identities.Email == nil || identities.Telegram != nil {
		t.Fatalf("missing TG: %+v %v", identities, err)
	}
	if identities.Email.EmailLocalPart != "Alice.Tag+one" || identities.Email.EmailDomain != "example.com" {
		t.Fatal("address fields diverged")
	}
	mustExec(t, db, "UPDATE `user` SET nickname='edited',avatar_url='custom' WHERE id=?", first.User.Id)
	again, err := s.ResolveOrCreateVerifiedEmail(ctx, "Alice.Tag+one@example.com")
	if err != nil || again.User.Id != first.User.Id || again.User.Nickname != "edited" || again.Binding != first.Binding {
		t.Fatalf("repeat: %+v %v", again, err)
	}
	codec, _ := idcodec.New("test-only-key123", idcodec.User)
	a, _ := codec.Encode(first.User.Id)
	b, _ := codec.Encode(again.User.Id)
	if a != b {
		t.Fatal("idx changed")
	}
	lower, err := s.ResolveOrCreateVerifiedEmail(ctx, "alice.Tag+one@example.com")
	if err != nil || lower.User.Id == first.User.Id {
		t.Fatalf("case folded: %v", err)
	}
	username := "old"
	photo := "https://example.com/a"
	tg, err := s.ResolveOrCreateVerifiedTelegram(ctx, userlogic.VerifiedTelegram{TelegramUserID: 9007199254740993, FirstName: "initial", Username: &username, PhotoURL: &photo})
	if err != nil {
		t.Fatal(err)
	}
	tgAgain, err := s.ResolveOrCreateVerifiedTelegram(ctx, userlogic.VerifiedTelegram{TelegramUserID: 9007199254740993, FirstName: "new external"})
	if err != nil || tgAgain.User.Id != tg.User.Id || tgAgain.User.Nickname != "initial" || tgAgain.User.AvatarUrl != photo {
		t.Fatalf("TG profile: %+v %v", tgAgain, err)
	}
	identities, err = s.FindCurrentIdentities(ctx, tg.User.Id)
	if err != nil || identities.Email != nil || identities.Telegram.Username.Valid || identities.Telegram.PhotoUrl.Valid || identities.Telegram.FirstName != "new external" {
		t.Fatalf("TG snapshot: %+v %v", identities, err)
	}
	check := func(binding userlogic.Binding) error {
		return transaction.New(db).TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			return s.CheckCurrentBindingInSession(ctx, session, binding)
		})
	}
	if err = check(first.Binding); err != nil {
		t.Fatal(err)
	}
	// Both tables start at ID 1: a wrong type must not use the other row's owner.
	wrong := first.Binding
	wrong.Type = model.IdentityTelegram
	if err = check(wrong); !errors.Is(err, userlogic.ErrBindingChanged) {
		t.Fatalf("cross-type binding: %v", err)
	}
	mustExec(t, db, "UPDATE email_identity SET user_id=NULL,bound_at=NULL,binding_version=binding_version+1 WHERE id=?", first.Binding.IdentityID)
	if err = check(first.Binding); !errors.Is(err, userlogic.ErrIdentityUnbound) {
		t.Fatalf("unbound: %v", err)
	}
	// Test-only simulation of the future binding use case under no concurrent writes.
	mustExec(t, db, "UPDATE email_identity SET user_id=?,bound_at=UTC_TIMESTAMP(6),binding_version=binding_version+1 WHERE id=?", first.User.Id, first.Binding.IdentityID)
	if err = check(first.Binding); !errors.Is(err, userlogic.ErrBindingChanged) {
		t.Fatalf("old generation accepted: %v", err)
	}
	mustExec(t, db, "UPDATE email_identity SET user_id=NULL,bound_at=NULL,binding_version=binding_version+1 WHERE id=?", first.Binding.IdentityID)
	rebound, err := s.ResolveOrCreateVerifiedEmail(ctx, "Alice.Tag+one@example.com")
	if err != nil || rebound.User.Id == first.User.Id || rebound.Binding.IdentityID != first.Binding.IdentityID || rebound.Binding.Version != 5 {
		t.Fatalf("new owner: %+v %v", rebound, err)
	}
	mustExec(t, db, "UPDATE `user` SET status=? WHERE id=?", model.StatusDisabled, rebound.User.Id)
	if result, err := s.ResolveOrCreateVerifiedEmail(ctx, "Alice.Tag+one@example.com"); result != nil || !errors.Is(err, userlogic.ErrDisabled) {
		t.Fatalf("disabled: %+v %v", result, err)
	}
	if _, err = s.FindUserByID(ctx, rebound.User.Id); err != nil {
		t.Fatal("disabled record unavailable for diagnostics", err)
	}
	if _, err = s.FindUserByID(ctx, 99999); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	invalid := first.Binding
	invalid.Type = "unknown"
	if err = check(invalid); !errors.Is(err, userlogic.ErrInvalidInput) {
		t.Fatal(err)
	}
	t.Log("email/TG resolution, independent product profile, idx stability, binding generations, disabled state verified")
}
func TestUserConcurrentRegistration(t *testing.T) {
	db, s := userDatabase(t, "user_concurrent")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, kind := range []model.IdentityType{model.IdentityEmail, model.IdentityTelegram} {
		start := make(chan struct{})
		results := make(chan *userlogic.Resolved, 12)
		failures := make(chan error, 12)
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var result *userlogic.Resolved
				var err error
				if kind == model.IdentityEmail {
					result, err = s.ResolveOrCreateVerifiedEmail(ctx, "race@example.com")
				} else {
					result, err = s.ResolveOrCreateVerifiedTelegram(ctx, userlogic.VerifiedTelegram{TelegramUserID: 42, FirstName: "race"})
				}
				if err != nil {
					failures <- err
				} else {
					results <- result
				}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		var owner int64
		for result := range results {
			if owner == 0 {
				owner = result.User.Id
			}
			if result.User.Id != owner || result.Binding.Version != 1 {
				t.Fatalf("different owners: %+v", result)
			}
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM `user`").Scan(&count); err != nil || count != 2 {
		t.Fatalf("orphan candidates: %d %v", count, err)
	}
	for _, table := range []string{"email_identity", "telegram_identity"} {
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("duplicate identities: %d %v", count, err)
		}
	}
}
func TestUserTransactionRollback(t *testing.T) {
	db, s := userDatabase(t, "user_rollback")
	ctx := context.Background()
	conn := sqlx.NewSqlConnFromDB(db)
	sentinel := errors.New("abort coordinated write")
	err := transaction.New(db).TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		users := model.NewUserModel(conn).WithSession(session)
		identities := model.NewEmailIdentityModel(conn).WithSession(session)
		result, err := users.Insert(ctx, &model.User{Status: model.StatusActive, AuthVersion: 1})
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = identities.Insert(ctx, &model.EmailIdentity{UserId: sql.NullInt64{Int64: id, Valid: true}, BindingVersion: 1, BoundAt: sql.NullTime{Time: time.Now().UTC(), Valid: true}, EmailAddress: "rollback@example.com", EmailLocalPart: "rollback", EmailDomain: "example.com", EmailOriginal: "rollback@example.com"})
		if err != nil {
			return err
		}
		if err = s.CheckCurrentBindingInSession(ctx, session, userlogic.Binding{Type: model.IdentityEmail, IdentityID: 1, UserID: id, Version: 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	for _, table := range []string{"user", "email_identity"} {
		var count int
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table)).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s: %d %v", table, count, err)
		}
	}
}

func TestUserMigrationCLIAndFailureState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	name, dsn, db, err := testRun.NewDatabase(ctx, "user_cli")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, action := range []string{"up", "up", "version"} {
		cmd := exec.CommandContext(ctx, "go", "run", "./tools/migrate", "--source", "business", "--database", name, "--action", action)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "DEXWALLET_MYSQL_DSN="+dsn)
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "version=2 dirty=false") {
			t.Fatalf("CLI %s: %v %s", action, err, output)
		}
	}
	example, err := migration.Open(dsn, name, "../../manifest/sql/diff/examples", "examples")
	if err != nil {
		t.Fatal(err)
	}
	if err = example.Up(); err != nil {
		t.Fatal(err)
	}
	example.Close()
	dir := t.TempDir()
	first, err := os.ReadFile("../../manifest/sql/diff/business/000001_create_user_model.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "000001_create_user_model.up.sql"), first, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile("../../manifest/sql/diff/business/000002_create_auth.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "000002_create_auth.up.sql"), second, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "000003_fail.up.sql"), []byte("CREATE TABLE business_partial (id INT); ALTER TABLE missing_business_table ADD COLUMN note INT;"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := migration.Open(dsn, name, dir, "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	version, dirty, err := m.Version()
	if err != nil || version != 2 || dirty {
		t.Fatalf("independent version: %d %t %v", version, dirty, err)
	}
	if err = m.Up(); err == nil {
		t.Fatal("expected DDL failure")
	}
	version, dirty, err = m.Version()
	if err != nil || version != 3 || !dirty {
		t.Fatalf("dirty state lost: %d %t %v", version, dirty, err)
	}
	var blocked migrate.ErrDirty
	if err = m.Up(); !errors.As(err, &blocked) {
		t.Fatalf("dirty retry allowed: %v", err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='business_partial'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial DDL hidden: %d %v", count, err)
	}
}
