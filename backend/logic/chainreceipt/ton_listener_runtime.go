package chainreceipt

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type TONListenerConfig struct {
	PollInterval     time.Duration `json:",default=5s"`
	OperationTimeout time.Duration `json:",default=20s"`
	RetryInterval    time.Duration `json:",default=1m"`
}

func (c TONListenerConfig) Check() error {
	if c.PollInterval < time.Second || c.OperationTimeout <= 0 || c.RetryInterval <= c.OperationTimeout {
		return fmt.Errorf("TON receipt listener requires poll >= 1s, positive timeout and retry interval exceeding timeout")
	}
	return nil
}

type TONListenerRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *TONListener) Start(ctx context.Context, c TONListenerConfig) (*TONListenerRuntime, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	var version int
	var dirty bool
	if err := s.db.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations_business").Scan(&version, &dirty); err != nil {
		return nil, err
	}
	if version < 33 || dirty {
		return nil, fmt.Errorf("TON receipt listener requires clean business schema >=33")
	}
	run, cancel := context.WithCancel(context.Background())
	rt := &TONListenerRuntime{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(rt.done)
		// Space discovery and verification calls on a public endpoint. Each tick
		// services one stream or candidate, including after a failure on another.
		scan := true
		for run.Err() == nil {
			op, stop := context.WithTimeout(run, c.OperationTimeout)
			var err error
			if scan {
				err = s.Scan(op, c.PollInterval)
			} else {
				err = s.Process(op, c.RetryInterval)
			}
			stop()
			if err != nil && run.Err() == nil {
				logx.Errorf("TON receipt listener (scan=%t): %v", scan, err)
			}
			scan = !scan
			timer := time.NewTimer(c.PollInterval)
			select {
			case <-run.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return rt, nil
}

func (r *TONListenerRuntime) Stop() { r.cancel() }
func (r *TONListenerRuntime) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
