package chainreceipt

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListenerConfig struct {
	PollInterval     time.Duration `json:",default=5s"`
	OperationTimeout time.Duration `json:",default=20s"`
	RetryInterval    time.Duration `json:",default=1m"`
}
type TONListenerConfig = ListenerConfig
type SolanaListenerConfig struct {
	ListenerConfig
	AnchorSlot int64
}

func (c SolanaListenerConfig) Check() error {
	if c.AnchorSlot <= 0 {
		return fmt.Errorf("Solana receipt listener requires a positive finalized anchor slot")
	}
	return c.ListenerConfig.Check()
}
func (c ListenerConfig) Check() error {
	if c.PollInterval < time.Second || c.OperationTimeout <= 0 || c.RetryInterval <= c.OperationTimeout {
		return fmt.Errorf("receipt listener requires poll >= 1s, positive timeout and retry interval exceeding timeout")
	}
	return nil
}

type ListenerRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type TONListenerRuntime = ListenerRuntime

func listenerSchema(ctx context.Context, db *sql.DB, version int) error {
	var current int
	var dirty bool
	if err := db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&current, &dirty); err != nil {
		return err
	}
	if current < version || dirty {
		return fmt.Errorf("receipt listener requires clean business schema >=%d", version)
	}
	return nil
}

type listenerPoll func(context.Context) (bool, error)

func runListener(network string, c ListenerConfig, polls ...listenerPoll) *ListenerRuntime {
	run, cancel := context.WithCancel(context.Background())
	r := &ListenerRuntime{cancel, make(chan struct{})}
	var group sync.WaitGroup
	for _, poll := range polls {
		group.Add(1)
		go func() {
			defer group.Done()
			for run.Err() == nil {
				op, stop := context.WithTimeout(run, c.OperationTimeout)
				busy, err := poll(op)
				stop()
				if err != nil && run.Err() == nil {
					logx.Errorf("%s receipt listener: %v", network, err)
				}
				if busy && err == nil {
					continue
				}
				timer := time.NewTimer(c.PollInterval)
				select {
				case <-run.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	go func() { group.Wait(); close(r.done) }()
	return r
}
func (s *TONListener) Start(ctx context.Context, c TONListenerConfig) (*TONListenerRuntime, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if err := listenerSchema(ctx, s.db, 33); err != nil {
		return nil, err
	}
	// Space discovery and verification on the public TON endpoint.
	scan := false
	return runListener("TON", c, func(ctx context.Context) (bool, error) {
		scan = !scan
		if scan {
			return false, s.Scan(ctx, c.PollInterval)
		}
		return false, s.Process(ctx, c.RetryInterval)
	}), nil
}
func (s *SolanaListener) Start(ctx context.Context, c SolanaListenerConfig) (*ListenerRuntime, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if err := listenerSchema(ctx, s.db, 34); err != nil {
		return nil, err
	}
	return runListener("Solana", c.ListenerConfig, func(ctx context.Context) (bool, error) { return s.Scan(ctx, c.AnchorSlot) }, func(ctx context.Context) (bool, error) { return s.Process(ctx, c.RetryInterval) }), nil
}
func (r *ListenerRuntime) Stop() { r.cancel() }
func (r *ListenerRuntime) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
