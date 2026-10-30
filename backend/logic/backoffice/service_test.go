package backoffice

import (
	"context"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestLostCommitRemainsUnknownAndIsNotRetried(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{store: &store{conn: sqlx.NewSqlConnFromDB(db)}, tx: transaction.New(db)}
	lost := errors.New("lost commit response")
	m.ExpectBegin()
	m.ExpectExec("insert into `merchant_user`").WithArgs("merchant@example.com", sqlmock.AnyArg(), "active", int64(1), int64(1)).WillReturnResult(sqlmock.NewResult(7, 1))
	m.ExpectExec("insert into `backoffice_audit`").WithArgs("super", int64(0), ActionCreate, "merchant", int64(7), "merchant@example.com", "", "active", "bootstrap", ResultSuccess, "test").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit().WillReturnError(lost)
	m.ExpectExec("insert into `backoffice_audit`").WithArgs("super", int64(0), ActionCreate, "merchant", int64(0), "merchant@example.com", "", "", "bootstrap", ResultUnknown, "test").WillReturnResult(sqlmock.NewResult(2, 1))
	result, err := s.Create(context.Background(), Actor{Realm: Super, TraceID: "test"}, Merchant, "merchant@example.com", "Temporary-password-123", "bootstrap")
	var failure *transaction.Failure
	if result != nil || !errors.As(err, &failure) || !failure.Unknown() || !errors.Is(err, lost) {
		t.Fatalf("uncertain commit became success/rejection: result=%v error=%v", result, err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
