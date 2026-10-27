package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadinessRecoveryFreshnessAndStop(t *testing.T) {
	now := time.Now()
	failure := errors.New("offline")
	checks := []Check{{Name: "mysql", Required: true, Run: func(context.Context) error { return nil }}, {Name: "cache", Run: func(context.Context) error { return failure }}}
	m := New(checks, 15*time.Second)
	m.now = func() time.Time { return now }
	if m.Ready() {
		t.Fatal("unchecked is ready")
	}
	m.Refresh(context.Background(), checks, time.Second, nil)
	if !m.Ready() {
		t.Fatal("optional cache broke readiness")
	}
	checks[0].Run = func(context.Context) error { return failure }
	m.Refresh(context.Background(), checks, time.Second, nil)
	if m.Ready() {
		t.Fatal("DB failure is ready")
	}
	checks[0].Run = func(context.Context) error { return nil }
	m.Refresh(context.Background(), checks, time.Second, nil)
	if !m.Ready() {
		t.Fatal("recovery not ready")
	}
	now = now.Add(16 * time.Second)
	if m.Ready() {
		t.Fatal("stale is ready")
	}
	m.Refresh(context.Background(), checks, time.Second, nil)
	m.Stop()
	if m.Ready() {
		t.Fatal("stopping is ready")
	}
}
func TestCheckCancellationIsNotSuccessful(t *testing.T) {
	checks := []Check{{Name: "db", Required: true, Run: func(ctx context.Context) error { <-ctx.Done(); return nil }}}
	m := New(checks, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Refresh(ctx, checks, time.Second, nil)
	if m.Ready() || m.Snapshot()[0].Err == nil {
		t.Fatal("cancelled check succeeded")
	}
}
