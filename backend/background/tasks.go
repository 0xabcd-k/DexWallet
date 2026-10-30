package background

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/0xabcd-k/DexWallet/backend/event"
	model "github.com/0xabcd-k/DexWallet/backend/model/background"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// A handler performs only same-database work using this session. External calls
// need their own business identity/reconciliation and are not retried by this API.
type Handler struct {
	Name    string
	Kind    event.Kind
	Version int64
	Run     func(context.Context, sqlx.Session, *model.EventOutbox) error
}

var handlerName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type retryError struct{ error }

func (e retryError) Unwrap() error { return e.error }
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryError{err}
}

type uncertainError struct{ error }

func (e uncertainError) Unwrap() error { return e.error }
func Uncertain(err error) error {
	if err == nil {
		return nil
	}
	return uncertainError{err}
}

// Receive acknowledges only durable acceptance. Its return does not mean that
// a handler succeeded. Upserts never reset an existing task or change its data.
func (s *Store) Receive(ctx context.Context, eventID int64, handlers []Handler) error {
	return s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		row, err := model.NewEventOutboxModel(sqlx.NewSqlConnFromSession(tx)).FindOne(ctx, eventID)
		if err != nil {
			return err
		}
		accepted := false
		for _, h := range handlers {
			if string(h.Kind) != row.Kind {
				continue
			}
			if h.Version != row.Version {
				return fmt.Errorf("unsupported event %d kind %s version %d", eventID, row.Kind, row.Version)
			}
			if _, err = tx.ExecCtx(ctx, "INSERT INTO background_task(event_id,handler,last_error,retry_reason) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=id", row.Id, h.Name, "", ""); err != nil {
				return err
			}
			accepted = true
		}
		if !accepted {
			return fmt.Errorf("no registered handler for event %d kind %s", eventID, row.Kind)
		}
		return nil
	})
}

// Execute fences expired owners before calling business code; the row remains
// locked until the business result and succeeded marker commit together.
func (s *Store) Execute(ctx context.Context, c Claim, h Handler) error {
	err := s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		var eventID int64
		err := tx.QueryRowCtx(ctx, &eventID, "SELECT event_id FROM background_task WHERE id=? AND generation=? AND state=? AND handler=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", c.ID, c.Generation, model.Running, h.Name)
		if errors.Is(err, sqlx.ErrNotFound) {
			return ErrLeaseLost
		}
		if err != nil {
			return err
		}
		row, err := model.NewEventOutboxModel(sqlx.NewSqlConnFromSession(tx)).FindOne(ctx, eventID)
		if err != nil {
			return err
		}
		if row.Kind != string(h.Kind) || row.Version != h.Version {
			return fmt.Errorf("task %d handler/event contract mismatch", c.ID)
		}
		if err = h.Run(ctx, tx, row); err != nil {
			return err
		}
		result, err := tx.ExecCtx(ctx, "UPDATE background_task SET state=?,lease_until=NULL,completed_at=UTC_TIMESTAMP(6) WHERE id=? AND generation=? AND state=?", model.Succeeded, c.ID, c.Generation, model.Running)
		return affected(result, err)
	})
	if err == nil || errors.Is(err, ErrLeaseLost) {
		return err
	}
	var failure *transaction.Failure
	if errors.As(err, &failure) && (failure.Unknown() || failure.Cleanup != nil) {
		// Never overwrite a succeeded marker after an indeterminate commit.
		// The next claimant locks and reads the durable state after lease expiry.
		return err
	}
	state := model.Dead
	var uncertain uncertainError
	var retry retryError
	var dbErr *mysql.MySQLError
	if errors.As(err, &uncertain) {
		state = model.Blocked
	} else if c.Attempts < s.config.MaxAttempts && (errors.As(err, &retry) || (errors.As(err, &dbErr) && (dbErr.Number == 1213 || dbErr.Number == 1205))) {
		state = model.Ready
	}
	// If cancellation prevents recording, the lease still makes the task recoverable.
	return errors.Join(err, s.finish(ctx, Tasks, c, state, err.Error()))
}
