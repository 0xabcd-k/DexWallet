package user

import (
	"context"
	"errors"
	"testing"
	"time"

	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var userColumns = []string{"id", "status", "nickname", "avatar_url", "auth_version", "created_at", "updated_at"}
var emailColumns = []string{"id", "user_id", "binding_version", "bound_at", "email_address", "email_local_part", "email_domain", "email_original", "created_at", "updated_at"}

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	conn := sqlx.NewSqlConnFromDB(db)
	return New(model.NewUserModel(conn), model.NewEmailIdentityModel(conn), model.NewTelegramIdentityModel(conn), transaction.New(db)), m
}
func TestCreateFailureRollsBackCandidateUser(t *testing.T) {
	s, m := mockService(t)
	failure := errors.New("database unavailable")
	m.ExpectQuery("select .*email_identity.*email_address").WillReturnRows(sqlmock.NewRows(emailColumns))
	m.ExpectBegin()
	m.ExpectExec("insert into `user`").WillReturnResult(sqlmock.NewResult(7, 1))
	now := time.Now()
	m.ExpectQuery("select .* from `user`").WillReturnRows(sqlmock.NewRows(userColumns).AddRow(7, model.StatusActive, "", "", 1, now, now))
	m.ExpectExec("insert into `email_identity`").WillReturnError(failure)
	m.ExpectRollback()
	result, err := s.ResolveOrCreateVerifiedEmail(context.Background(), normalizedEmail(t, "a@example.com"))
	if result != nil || !errors.Is(err, failure) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestCommitUnknownIsNotRetriedOrReturnedAsSuccess(t *testing.T) {
	s, m := mockService(t)
	now := time.Now()
	failure := errors.New("lost commit response")
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows(emailColumns).AddRow(3, 7, 1, now, "a@example.com", "a", "example.com", "a@example.com", now, now)
	}
	m.ExpectQuery("select .*email_identity.*email_address").WillReturnRows(row())
	m.ExpectBegin()
	m.ExpectQuery("select .* from `user`.*for update").WillReturnRows(sqlmock.NewRows(userColumns).AddRow(7, model.StatusActive, "", "", 1, now, now))
	m.ExpectQuery("select .*email_identity.*for update").WillReturnRows(row())
	m.ExpectCommit().WillReturnError(failure)
	result, err := s.ResolveOrCreateVerifiedEmail(context.Background(), normalizedEmail(t, "a@example.com"))
	var txErr *transaction.Failure
	if result != nil || !errors.As(err, &txErr) || !txErr.Unknown() || !errors.Is(err, failure) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestQueryFailureIsNotMissingIdentity(t *testing.T) {
	s, m := mockService(t)
	failure := errors.New("read failed")
	m.ExpectQuery("select .*email_identity.*email_address").WillReturnError(failure)
	result, err := s.ResolveOrCreateVerifiedEmail(context.Background(), normalizedEmail(t, "a@example.com"))
	if result != nil || !errors.Is(err, failure) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestResolutionRechecksOwnerAfterLocking(t *testing.T) {
	s, m := mockService(t)
	now := time.Now()
	row := func(owner int64) *sqlmock.Rows {
		return sqlmock.NewRows(emailColumns).AddRow(3, owner, 4, now, "a@example.com", "a", "example.com", "a@example.com", now, now)
	}
	for attempt := 0; attempt < 2; attempt++ {
		owner := int64(attempt + 7)
		m.ExpectQuery("select .*email_identity.*email_address").WillReturnRows(row(owner))
		m.ExpectBegin()
		m.ExpectQuery("select .* from `user`.*for update").WithArgs(owner).WillReturnRows(sqlmock.NewRows(userColumns).AddRow(owner, model.StatusActive, "", "", 1, now, now))
		// A concurrent rebind won before our transaction took the identity lock.
		m.ExpectQuery("select .*email_identity.*for update").WillReturnRows(row(8))
		if attempt == 0 {
			m.ExpectRollback()
		} else {
			m.ExpectCommit()
		}
	}
	result, err := s.ResolveOrCreateVerifiedEmail(context.Background(), normalizedEmail(t, "a@example.com"))
	if err != nil || result.User.Id != 8 || result.Binding.UserID != 8 {
		t.Fatalf("stale ownership used: %+v %v", result, err)
	}
}

func normalizedEmail(t *testing.T, raw string) EmailAddress {
	t.Helper()
	address, err := NormalizeEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	return address
}
