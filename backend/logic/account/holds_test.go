package account

import (
	"context"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	model "github.com/0xabcd-k/DexWallet/backend/model/account"
	driver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
)

func TestHoldCommandsRequireCallerTransaction(t *testing.T) {
	service := New(&accountStore{})
	if _, err := service.Hold(context.Background(), nil, HoldRequest{UserID: 1, BusinessType: BusinessWithdrawal, BusinessID: 1, Amount: decimal.NewFromInt(1)}); err == nil {
		t.Fatal("hold without transaction")
	}
	if _, err := service.Consume(context.Background(), nil, 1, []Entry{Credit(2, model.KindUSD, decimal.NewFromInt(1))}); err == nil {
		t.Fatal("consume without transaction")
	}
	if _, err := service.Release(context.Background(), nil, 1, model.HoldCancelled); err == nil {
		t.Fatal("release without transaction")
	}
	if _, err := service.Release(context.Background(), nil, 1, "timeout"); !errors.Is(err, ErrInvalidHold) {
		t.Fatal("timeout is not release evidence", err)
	}
}
func TestHoldQueryRejectsInvalidInputsAndPreservesStorageFailure(t *testing.T) {
	failure := errors.New("database unavailable")
	service := New(&accountStore{err: failure})
	if _, err := service.Holds(context.Background(), 1, "", 0, 20); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, input := range []struct {
		user   int64
		status string
		before int64
		limit  int
	}{{0, "", 0, 20}, {1, "expired", 0, 20}, {1, "held", -1, 20}, {1, "", 0, 0}, {1, "", 0, 101}} {
		_, err := service.Holds(context.Background(), input.user, input.status, input.before, input.limit)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != apperror.InvalidArgument {
			t.Fatal(input, err)
		}
	}
}
func TestLockFailureRetainsDatabaseCause(t *testing.T) {
	for _, code := range []uint16{mysqlLockWaitTimeout, mysqlDeadlock} {
		cause := &driver.MySQLError{Number: code, Message: "test conflict"}
		err := classifyWriteError(cause)
		if !errors.Is(err, ErrAccountBusy) || !errors.Is(err, cause) {
			t.Fatal("lost lock failure semantics", err)
		}
	}
	cause := errors.New("lost commit response")
	if classifyWriteError(cause) != cause {
		t.Fatal("unrelated failure changed")
	}
}
