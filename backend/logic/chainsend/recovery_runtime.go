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
	runOne           func(context.Context) (bool, error)
	checkSchema      func(context.Context) error
	policy           RecoveryPolicy
	network          string
	mu               sync.Mutex
	started, stopped bool
	stop, done       chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

func NewRecoveryRuntime[T any](r interface {
	RunOne(context.Context) (bool, error)
	List(context.Context, string, int) ([]T, error)
	schedule() *recoveryStore
}) *RecoveryRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	store := r.schedule()
	return &RecoveryRuntime{
		runOne: r.RunOne,
		checkSchema: func(ctx context.Context) error {
			_, err := r.List(ctx, "", 1)
			return err
		},
		policy: store.config, network: store.network,
		stop: make(chan struct{}), done: make(chan struct{}),
		ctx: ctx, cancel: cancel,
	}
}
func (r *RecoveryRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return fmt.Errorf("%s recovery runtime already started or stopped", r.network)
	}
	ctx, cancel := context.WithTimeout(ctx, r.policy.OperationTimeout)
	defer cancel()
	if err := r.checkSchema(ctx); err != nil {
		return fmt.Errorf("%s recovery schema: %w", r.network, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.started = true
	for i := 0; i < r.policy.Workers; i++ {
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
		ctx, cancel := context.WithTimeout(r.ctx, r.policy.OperationTimeout)
		work, err := r.runOne(ctx)
		cancel()
		r.wg.Done()
		if err != nil {
			// Pending observations are expected; failures and exhausted/manual-review
			// records remain diagnostic, with the original key without signed payloads.
			if errors.Is(err, chainreceipt.ErrUnconfirmed) && !errors.Is(err, ErrRecoveryBudget) && !errors.Is(err, ErrRecoveryLeaseLost) {
				logx.Infof("%s recovery pending: %v", r.network, err)
			} else {
				logx.Errorf("%s recovery: %v", r.network, err)
			}
		}
		if !work || err != nil {
			timer := time.NewTimer(r.policy.PollInterval)
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
