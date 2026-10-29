package svc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestCloseWaitsForProbeBeforeClosingClients(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The last probe still uses the client after observing cancellation. Closing
	// the DB before that probe completes would make its Ping fail.
	mock.ExpectPing()
	closeFailure := errors.New("close diagnostic")
	mock.ExpectClose().WillReturnError(closeFailure)
	started := make(chan struct{})
	probeResult := make(chan error, 1)
	var calls atomic.Int32
	s := &ServiceContext{
		Config: config.Config{ProbeInterval: time.Millisecond, ProbeTimeout: time.Second, ProbeFreshness: time.Minute},
		DB:     db,
		checks: []health.Check{{Name: "fake", Required: true, Run: func(ctx context.Context) error {
			switch calls.Add(1) {
			case 1:
				return nil
			case 2:
				close(started)
			default:
				return ctx.Err()
			}
			<-ctx.Done()
			probeResult <- db.PingContext(context.Background())
			return ctx.Err()
		}}},
	}
	if err = s.startHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if !s.Health.Ready() {
		t.Fatal("initial health check did not run before startup returned")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("periodic probe did not start")
	}
	for range 2 {
		if err = s.Close(); !errors.Is(err, closeFailure) {
			t.Fatalf("close failure was lost: %v", err)
		}
	}
	if err = <-probeResult; err != nil {
		t.Fatalf("client closed while probe was still running: %v", err)
	}
	if s.Health.Ready() {
		t.Fatal("closed service is still ready")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCancellationDuringFirstProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &ServiceContext{
		Config: config.Config{ProbeTimeout: time.Second, ProbeFreshness: time.Minute},
		checks: []health.Check{{Name: "fake", Required: true, Run: func(ctx context.Context) error {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}}},
	}
	if err := s.startHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup cancellation was lost: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Health.Ready() {
		t.Fatal("cancelled startup is ready")
	}
}
