// Package transaction preserves phase and causes at the SQL transaction boundary.
package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Failure struct {
	Phase   string
	Cause   error
	Cleanup error
	Stack   []byte
}

func (e *Failure) Error() string {
	return fmt.Sprintf("transaction %s: %v; rollback: %v", e.Phase, e.Cause, e.Cleanup)
}
func (e *Failure) Unwrap() []error {
	var causes []error
	if e.Cause != nil {
		causes = append(causes, e.Cause)
	}
	if e.Cleanup != nil {
		causes = append(causes, e.Cleanup)
	}
	return causes
}

// Commit failures may be indeterminate. Callers must not retry blindly.
func (e *Failure) Unknown() bool { return e.Phase == "commit" }

type tx interface {
	sqlx.Session
	Commit() error
	Rollback() error
}
type activeTransaction struct{}

type Runner struct {
	begin func(context.Context) (tx, error)
}
type sqlTx struct {
	sqlx.Session
	raw *sql.Tx
}

func (t sqlTx) Commit() error   { return t.raw.Commit() }
func (t sqlTx) Rollback() error { return t.raw.Rollback() }
func New(db *sql.DB) *Runner {
	return &Runner{begin: func(ctx context.Context) (tx, error) {
		t, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		return sqlTx{Session: sqlx.NewSessionFromTx(t), raw: t}, nil
	}}
}

// TransactCtx uses go-zero sessions with database/sql ownership. This small
// adapter preserves both errors, phases, panic stacks and BeginTx cancellation.
func (r *Runner) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) (err error) {
	if ctx.Value(activeTransaction{}) != nil {
		return &Failure{Phase: "begin", Cause: errors.New("nested transaction is not supported")}
	}
	if err = ctx.Err(); err != nil {
		return &Failure{Phase: "begin", Cause: err}
	}
	t, err := r.begin(ctx)
	if err != nil {
		return &Failure{Phase: "begin", Cause: err}
	}
	defer func() {
		if p := recover(); p != nil {
			cause, ok := p.(error)
			if !ok {
				cause = fmt.Errorf("%v", p)
			}
			err = &Failure{Phase: "panic", Cause: cause, Cleanup: t.Rollback(), Stack: debug.Stack()}
		}
	}()
	if err = fn(context.WithValue(ctx, activeTransaction{}, true), t); err == nil {
		err = ctx.Err()
	}
	if err != nil {
		cleanup := t.Rollback()
		if cleanup != nil && !errors.Is(cleanup, sql.ErrTxDone) {
			return &Failure{Phase: "rollback", Cause: err, Cleanup: cleanup}
		}
		return err
	}
	if err = t.Commit(); err != nil {
		return &Failure{Phase: "commit", Cause: err}
	}
	return nil
}
