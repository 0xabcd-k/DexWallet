package transfer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type txFunc func(context.Context, func(context.Context, sqlx.Session) error) error

func (f txFunc) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	return f(ctx, fn)
}

func TestRejectInvalidTransferBeforeWriting(t *testing.T) {
	actor := userlogic.Actor{UserID: 1, AuthVersion: 1}
	base := Request{Key: "request_0123456789", RecipientID: 2, Amount: decimal.NewFromInt(10)}
	s := New(nil, txFunc(func(context.Context, func(context.Context, sqlx.Session) error) error {
		t.Fatal("invalid request reached database transaction")
		return nil
	}), nil)
	for _, key := range []string{"", "short", strings.Repeat("x", 65), " request_0123456789", "request_012345678!"} {
		r := base
		r.Key = key
		if _, err := s.Submit(context.Background(), actor, r); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(key, err)
		}
	}
	for _, amount := range []string{"0", "-1", "1.001", "1000000000000000000"} {
		r := base
		r.Amount = decimal.RequireFromString(amount)
		if _, err := s.Submit(context.Background(), actor, r); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(amount, err)
		}
	}
	for _, id := range []int64{0, -1, 1} {
		r := base
		r.RecipientID = id
		if _, err := s.Submit(context.Background(), actor, r); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(id, err)
		}
	}
	if _, err := s.Submit(context.Background(), userlogic.Actor{UserID: 1}, base); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
}

func TestOnlyRolledBackDeadlocksRetryOnce(t *testing.T) {
	deadlock := &mysql.MySQLError{Number: 1213, Message: "deadlock victim"}
	timeout := &mysql.MySQLError{Number: 1205, Message: "lock wait"}
	cleanup := &transaction.Failure{Phase: "rollback", Cause: deadlock, Cleanup: errors.New("connection lost")}
	for _, tc := range []struct {
		name  string
		cause error
		calls int
		want  error
	}{
		{"deadlock", deadlock, 2, ErrBusy},
		{"timeout", timeout, 1, ErrBusy},
		{"failed_cleanup", cleanup, 1, cleanup},
		{"business_rejection", account.ErrInsufficientBalance, 1, account.ErrInsufficientBalance},
		{"connection_error", errors.New("connection lost"), 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := New(nil, txFunc(func(context.Context, func(context.Context, sqlx.Session) error) error { calls++; return tc.cause }), nil)
			result, err := s.Submit(context.Background(), userlogic.Actor{UserID: 1, AuthVersion: 1}, Request{"request_0123456789", 2, decimal.NewFromInt(10)})
			if result != nil || !errors.Is(err, tc.cause) || (tc.want != nil && !errors.Is(err, tc.want)) || calls != tc.calls {
				t.Fatal(result, err, calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	s := New(nil, txFunc(func(context.Context, func(context.Context, sqlx.Session) error) error {
		calls++
		cancel()
		return deadlock
	}), nil)
	_, err := s.Submit(ctx, userlogic.Actor{UserID: 1, AuthVersion: 1}, Request{"request_0123456789", 2, decimal.NewFromInt(10)})
	if calls != 1 || !errors.Is(err, deadlock) {
		t.Fatal(calls, err)
	}
}

func TestUnknownCommitAndFailedLookupRemainUnknown(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	readErr := errors.New("primary unavailable")
	commitErr := &transaction.Failure{Phase: "commit", Cause: errors.New("commit response lost")}
	mock.ExpectQuery("select .* from `user`").WithArgs(int64(1)).WillReturnError(readErr)
	calls := 0
	s := New(sqlx.NewSqlConnFromDB(db), txFunc(func(context.Context, func(context.Context, sqlx.Session) error) error { calls++; return commitErr }), nil)
	result, err := s.Submit(context.Background(), userlogic.Actor{UserID: 1, AuthVersion: 1}, Request{"request_0123456789", 2, decimal.NewFromInt(10)})
	if result != nil || calls != 1 || !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, commitErr) || !errors.Is(err, readErr) {
		t.Fatal(result, err, calls)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
