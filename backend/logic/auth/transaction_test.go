package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestOperationCommitUnknownNeverReturnsSuccessOrRetries(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn := sqlx.NewSqlConnFromDB(db)
	now := time.Now().UTC()
	cfg := DefaultConfig()
	cfg.Secret = "test-only-auth-secret-32-characters"
	s := &Service{cfg: cfg, operations: model.NewAuthOperationModel(conn), tx: transaction.New(db), now: func() time.Time { return now }}
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO auth_operation").WillReturnResult(sqlmock.NewResult(1, 1))
	columns := []string{"id", "request_digest", "input_digest", "proof_digest", "purpose", "identity_type", "actor_id", "actor_version", "user_id", "identity_id", "binding_version", "auth_version", "completed", "expires_at", "created_at", "updated_at"}
	m.ExpectQuery("SELECT .* FROM auth_operation.*FOR UPDATE").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, "digest", "input", nil, "login", "email", 0, 0, 0, 0, 0, 0, false, now.Add(time.Minute), now, now))
	m.ExpectExec("update `auth_operation`").WillReturnResult(sqlmock.NewResult(0, 1))
	lost := errors.New("lost commit acknowledgement")
	m.ExpectCommit().WillReturnError(lost)
	calls := 0
	result, err := s.run(context.Background(), "0123456789012345678901234567890123456789012345678901234567890123", Intent{Purpose: userlogic.Login}, usermodel.IdentityEmail, "input", func(context.Context, sqlx.Session) (*userlogic.Resolved, string, error) {
		calls++
		return &userlogic.Resolved{User: &usermodel.User{Id: 7, AuthVersion: 1}, Binding: userlogic.Binding{Type: usermodel.IdentityEmail, UserID: 7, IdentityID: 3, Version: 1}}, "proof", nil
	})
	var failure *transaction.Failure
	if result != nil || calls != 1 || !errors.As(err, &failure) || !failure.Unknown() || !errors.Is(err, lost) {
		t.Fatalf("result=%+v calls=%d error=%v", result, calls, err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
