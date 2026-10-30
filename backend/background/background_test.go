package background

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/event"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	model "github.com/0xabcd-k/DexWallet/backend/model/background"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestEnvelopeUsesStableIdxAndRejectsMalformedIdentity(t *testing.T) {
	codec, err := idcodec.New("test-only-key123", idcodec.Event)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := codec.Encode(9007199254740993)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(Envelope{EnvelopeVersion, idx})
	if id, err := DecodeEnvelope(body, codec); err != nil || id != 9007199254740993 {
		t.Fatal(id, err)
	}
	for _, body := range []string{"{}", "not-json", `{"version":2,"eventIdx":"` + idx + `"}`, `{"version":1,"eventIdx":"invalid"}`, strings.Repeat(" ", 513)} {
		if _, err := DecodeEnvelope([]byte(body), codec); err == nil {
			t.Fatal(body)
		}
	}
}
func TestBoundedPolicy(t *testing.T) {
	c := Defaults()
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
	if c.delay(1) != 5*time.Second || c.delay(3) != 20*time.Second || c.delay(100) != 5*time.Minute {
		t.Fatal("retry backoff outside policy")
	}
	for _, change := range []func(*Config){func(c *Config) { c.LeaseDuration = c.OperationTimeout }, func(c *Config) { c.MaxAttempts = 0 }, func(c *Config) { c.Workers = 0 }, func(c *Config) { c.RetryBase = 0 }} {
		bad := c
		change(&bad)
		if bad.Check() == nil {
			t.Fatal("invalid policy")
		}
	}
}
func TestStopBeforeStartAndIdempotentDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	r.Stop()
	r.Stop()
	if err := r.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.begin() || ctx.Err() == nil {
		t.Fatal("stopped runtime admitted new work")
	}
}

func TestTaskUnknownCommitDoesNotOverwriteResultOrRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewStore(db, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT event_id FROM background_task").WithArgs(int64(1), int64(1), model.Running, "fixture").WillReturnRows(sqlmock.NewRows([]string{"event_id"}).AddRow(1))
	columns := []string{"id", "kind", "business_id", "version", "payload", "state", "attempts", "generation", "next_at", "lease_until", "last_error", "retry_reason", "retry_operator", "retried_at", "created_at", "completed_at"}
	now := time.Now().UTC()
	mock.ExpectQuery("select .* from `event_outbox`").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows(columns).AddRow(1, string(event.TransferCompleted), 1, 1, "{}", model.Published, 1, 1, now, nil, "", "", "", nil, now, now))
	mock.ExpectExec("UPDATE background_task SET state=").WithArgs(model.Succeeded, int64(1), int64(1), model.Running).WillReturnResult(sqlmock.NewResult(0, 1))
	lost := errors.New("commit response lost")
	mock.ExpectCommit().WillReturnError(lost)
	calls := 0
	h := Handler{Name: "fixture", Kind: event.TransferCompleted, Version: 1, Run: func(context.Context, sqlx.Session, *model.EventOutbox) error { calls++; return nil }}
	err = store.Execute(context.Background(), Claim{ID: 1, Attempts: 1, Generation: 1}, h)
	var failure *transaction.Failure
	if calls != 1 || !errors.Is(err, lost) || !errors.As(err, &failure) || !failure.Unknown() {
		t.Fatal(calls, err)
	}
	// No subsequent SQL may mark this possibly committed task failed or ready.
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
