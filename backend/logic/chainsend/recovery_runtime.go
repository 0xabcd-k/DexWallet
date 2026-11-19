package chainsend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/zeromicro/go-zero/core/logx"
)

type RecoveryRuntime struct {
	recovery         *Recovery
	mu               sync.Mutex
	started, stopped bool
	stop, done       chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

func NewRecoveryRuntime(r *Recovery) *RecoveryRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &RecoveryRuntime{recovery: r, stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}
}
func (r *RecoveryRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return fmt.Errorf("TON recovery runtime already started or stopped")
	}
	ctx, cancel := context.WithTimeout(ctx, r.recovery.store.config.OperationTimeout)
	defer cancel()
	if _, err := r.recovery.List(ctx, "", 1); err != nil {
		return fmt.Errorf("TON recovery schema: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.started = true
	for i := 0; i < r.recovery.store.config.Workers; i++ {
		r.wg.Add(1)
		go r.loop()
	}
	go func() { r.wg.Wait(); r.cancel(); close(r.done) }()
	return nil
}
func (r *RecoveryRuntime) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}
	r.wg.Add(1)
	return true
}
func (r *RecoveryRuntime) loop() {
	defer r.wg.Done()
	for {
		if !r.begin() {
			return
		}
		ctx, cancel := context.WithTimeout(r.ctx, r.recovery.store.config.OperationTimeout)
		work, err := r.recovery.RunOne(ctx)
		cancel()
		r.wg.Done()
		if err != nil {
			// Pending observations are expected; failures and exhausted/manual-review
			// records remain diagnostic, with the original key but no signed BoC.
			if errors.Is(err, chainreceipt.ErrUnconfirmed) && !errors.Is(err, ErrRecoveryBudget) && !errors.Is(err, ErrRecoveryLeaseLost) {
				logx.Infof("TON recovery pending: %v", err)
			} else {
				logx.Errorf("TON recovery: %v", err)
			}
		}
		if !work || err != nil {
			timer := time.NewTimer(r.recovery.store.config.PollInterval)
			select {
			case <-r.stop:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
func (r *RecoveryRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	close(r.stop)
	if !r.started {
		r.cancel()
		close(r.done)
	}
}
func (r *RecoveryRuntime) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		r.cancel()
		return ctx.Err()
	}
}
