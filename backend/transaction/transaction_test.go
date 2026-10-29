package transaction

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestCommitAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(1, 1))
			cause := errors.New("business rejected")
			if fail {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err = New(db).TransactCtx(context.Background(), func(ctx context.Context, s sqlx.Session) error {
				if _, e := s.ExecCtx(ctx, "INSERT INTO sample VALUES (1)"); e != nil {
					return e
				}
				if fail {
					return cause
				}
				return nil
			})
			if fail && !errors.Is(err, cause) || !fail && err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestFailuresPreservePhaseAndCausesWithoutRetries(t *testing.T) {
	for _, phase := range []string{"begin", "commit", "rollback", "panic"} {
		t.Run(phase, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			original := errors.New("original")
			cleanup := errors.New("cleanup")
			begin := m.ExpectBegin()
			if phase == "begin" {
				begin.WillReturnError(original)
			}
			switch phase {
			case "commit":
				m.ExpectCommit().WillReturnError(original)
			case "rollback":
				m.ExpectRollback().WillReturnError(cleanup)
			case "panic":
				m.ExpectRollback()
			}
			calls := 0
			err = New(db).TransactCtx(context.Background(), func(context.Context, sqlx.Session) error {
				calls++
				switch phase {
				case "rollback":
					return original
				case "panic":
					panic("boom")
				}
				return nil
			})
			var e *Failure
			if !errors.As(err, &e) || e.Phase != phase {
				t.Fatalf("%s: %v", phase, err)
			}
			if phase == "rollback" && (!errors.Is(err, original) || !errors.Is(err, cleanup)) {
				t.Fatal("cause lost")
			}
			if phase == "panic" && len(e.Stack) == 0 {
				t.Fatal("no panic stack")
			}
			if e.Unknown() != (phase == "commit") {
				t.Fatal("incorrect outcome certainty")
			}
			if calls > 1 || phase == "begin" && calls != 0 {
				t.Fatal("callback retried")
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCancelledBeforeBegin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Runner{}).TransactCtx(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNestedTransactionRejectedBeforeSecondBegin(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.ExpectBegin()
	m.ExpectRollback()
	r := New(db)
	err = r.TransactCtx(context.Background(), func(ctx context.Context, _ sqlx.Session) error {
		return r.TransactCtx(ctx, func(context.Context, sqlx.Session) error { return nil })
	})
	if err == nil {
		t.Fatal("nested transaction accepted")
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidationAfterCommitIncludingUnknownOutcome(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "unknown"}[uncertain], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			commit := mock.ExpectCommit()
			lost := errors.New("commit response lost")
			if uncertain {
				commit.WillReturnError(lost)
			}
			calls := 0
			err = New(db).TransactCtx(context.Background(), func(ctx context.Context, _ sqlx.Session) error {
				for i := 0; i < 2; i++ {
					if err := AfterCommit(ctx, "same-user", func() { calls++ }); err != nil {
						return err
					}
				}
				if calls != 0 {
					t.Fatal("invalidation before commit")
				}
				return nil
			})
			if calls != 1 {
				t.Fatalf("invalidation calls=%d", calls)
			}
			if uncertain {
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Unknown() || !errors.Is(err, lost) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
