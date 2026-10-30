package background

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	model "github.com/0xabcd-k/DexWallet/backend/model/background"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Queue string

const (
	Events Queue = "events"
	Tasks  Queue = "tasks"
)

var ErrLeaseLost = errors.New("background claim no longer owned")
var ErrRetryDenied = errors.New("background record cannot be retried in its current state")

func (q Queue) table() (string, error) {
	switch q {
	case Events:
		return "event_outbox", nil
	case Tasks:
		return "background_task", nil
	}
	return "", fmt.Errorf("unknown background queue %q", q)
}

type Store struct {
	conn    sqlx.SqlConn
	tx      *transaction.Runner
	claimTx *transaction.Runner
	config  Config
}

func NewStore(db *sql.DB, c Config) (*Store, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	return &Store{conn: sqlx.NewSqlConnFromDB(db), tx: transaction.New(db), claimTx: transaction.NewWithIsolation(db, sql.LevelReadCommitted), config: c}, nil
}

type Claim struct {
	ID         int64 `db:"id"`
	Attempts   int64 `db:"attempts"`
	Generation int64 `db:"generation"`
}

// Claim uses DB time and short row locks; no network or handler runs while this
// transaction is open. Generation never resets, including after manual retry.
func (s *Store) Claim(ctx context.Context, q Queue, handler string) (*Claim, error) {
	table, err := q.table()
	if err != nil {
		return nil, err
	}
	var result *Claim
	err = s.claimTx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		// Separate indexed scans avoid the OR/filesort locking every due row.
		// Recover expired claims before taking fresh work.
		var row Claim
		var err error
		for _, candidate := range []struct{ state, clock string }{{model.Running, "lease_until"}, {model.Ready, "next_at"}} {
			filter := ""
			args := []any{candidate.state}
			if q == Tasks {
				filter = " AND handler=?"
				args = append(args, handler)
			}
			err = tx.QueryRowCtx(ctx, &row, "SELECT id,attempts,generation FROM "+table+" WHERE state=? AND "+candidate.clock+"<=UTC_TIMESTAMP(6)"+filter+" ORDER BY "+candidate.clock+",id LIMIT 1 FOR UPDATE SKIP LOCKED", args...)
			if !errors.Is(err, sqlx.ErrNotFound) {
				break
			}
		}
		if errors.Is(err, sqlx.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Attempts >= s.config.MaxAttempts {
			_, err = tx.ExecCtx(ctx, "UPDATE "+table+" SET state=?,lease_until=NULL,last_error=CONCAT(last_error, '\\nlease expired after attempt limit') WHERE id=?", model.Dead, row.ID)
			return err
		}
		_, err = tx.ExecCtx(ctx, "UPDATE "+table+" SET state=?,attempts=attempts+1,generation=generation+1,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", model.Running, s.config.LeaseDuration.Microseconds(), row.ID)
		if err != nil {
			return err
		}
		row.Attempts++
		row.Generation++
		result = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) Event(ctx context.Context, id int64) (*model.EventOutbox, error) {
	return model.NewEventOutboxModel(s.conn).FindOne(ctx, id)
}
func (s *Store) Task(ctx context.Context, id int64) (*model.BackgroundTask, error) {
	return model.NewBackgroundTaskModel(s.conn).FindOne(ctx, id)
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) finish(ctx context.Context, q Queue, c Claim, state, detail string) error {
	table, err := q.table()
	if err != nil {
		return err
	}
	// Completion time is assigned in SQL, rather than from a worker's clock.
	stamp := "NULL"
	if state == model.Published || state == model.Succeeded {
		stamp = "UTC_TIMESTAMP(6)"
	}
	delay := s.config.delay(c.Attempts).Microseconds()
	result, err := s.conn.ExecCtx(ctx, "UPDATE "+table+" SET state=?,lease_until=NULL,next_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error=?,completed_at="+stamp+" WHERE id=? AND generation=? AND state=? AND lease_until>UTC_TIMESTAMP(6)", state, delay, detail, c.ID, c.Generation, model.Running)
	return affected(result, err)
}

// Retry retains the original identity, last error and increasing generation.
// Blocked/unknown tasks require business-specific reconciliation, not blind retry.
func (s *Store) Retry(ctx context.Context, q Queue, id int64, operator, reason string) error {
	table, err := q.table()
	if err != nil {
		return err
	}
	if id <= 0 || strings.TrimSpace(operator) == "" || len(operator) > 128 || strings.TrimSpace(reason) == "" || len(reason) > 4096 {
		return fmt.Errorf("retry requires id, operator and reason (up to 4096 bytes)")
	}
	allowed := "state=?"
	args := []any{model.Ready, operator, reason, id, model.Dead}
	if q == Events {
		allowed = "state IN (?,?)"
		args = append(args, model.Published)
	}
	result, err := s.conn.ExecCtx(ctx, "UPDATE "+table+" SET state=?,attempts=0,next_at=UTC_TIMESTAMP(6),lease_until=NULL,completed_at=NULL,retry_operator=?,retry_reason=?,retried_at=UTC_TIMESTAMP(6) WHERE id=? AND "+allowed, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRetryDenied
	}
	return nil
}

type Record struct {
	ID            int64        `db:"id"`
	State         string       `db:"state"`
	Attempts      int64        `db:"attempts"`
	Generation    int64        `db:"generation"`
	LastError     string       `db:"last_error"`
	RetryOperator string       `db:"retry_operator"`
	RetryReason   string       `db:"retry_reason"`
	EventID       int64        `db:"event_id"`
	Kind          string       `db:"kind"`
	BusinessID    int64        `db:"business_id"`
	Handler       string       `db:"handler"`
	NextAt        time.Time    `db:"next_at"`
	LeaseUntil    sql.NullTime `db:"lease_until"`
}

func (s *Store) List(ctx context.Context, q Queue, state string, limit int) ([]Record, error) {
	table, err := q.table()
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be 1..100")
	}
	filter := ""
	args := []any{}
	if state != "" {
		filter = " WHERE r.state=?"
		args = append(args, state)
	}
	args = append(args, limit)
	var result []Record
	source := table + " r"
	identity := "r.id AS event_id,r.kind,r.business_id,'' AS handler"
	if q == Tasks {
		source += " JOIN event_outbox e ON e.id=r.event_id"
		identity = "r.event_id,e.kind,e.business_id,r.handler"
	}
	err = s.conn.QueryRowsCtx(ctx, &result, "SELECT r.id,r.state,r.attempts,r.generation,r.last_error,r.retry_operator,r.retry_reason,r.next_at,r.lease_until,"+identity+" FROM "+source+filter+" ORDER BY r.id DESC LIMIT ?", args...)
	return result, err
}
