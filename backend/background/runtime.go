package background

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

type Runtime struct {
	store     *Store
	sender    Sender
	codec     *idcodec.Codec
	namespace string
	handlers  []Handler
	consumer  rocketmq.PushConsumer
	mu        sync.Mutex
	started   bool
	stopped   bool
	stop      chan struct{}
	done      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeErr  error
}

func NewRuntime(s *Store, sender Sender, codec *idcodec.Codec, namespace string, handlers []Handler) (*Runtime, error) {
	if s == nil || sender == nil || codec == nil || namespace == "" {
		return nil, fmt.Errorf("background runtime requires store, sender, codec and namespace")
	}
	names := map[string]bool{}
	for _, h := range handlers {
		if !handlerName.MatchString(h.Name) || h.Kind == "" || h.Version <= 0 || h.Run == nil || names[h.Name] {
			return nil, fmt.Errorf("invalid or duplicate background handler %q", h.Name)
		}
		names[h.Name] = true
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{store: s, sender: sender, codec: codec, namespace: namespace, handlers: append([]Handler{}, handlers...), stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}, nil
}

// Start owns one optional consumer plus separately bounded publisher/task pools.
// Production currently publishes transfer facts; real notification handlers are
// registered when those channels exist. Tests register isolated SQL handlers.
func (r *Runtime) Start(ctx context.Context, names []string, credentials primitive.Credentials) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return fmt.Errorf("background runtime already started or stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Fail clearly on missing migrations; opening the API must not silently create tables.
	if _, err := r.store.List(ctx, Events, "", 1); err != nil {
		return fmt.Errorf("background schema: %w", err)
	}
	if _, err := r.store.List(ctx, Tasks, "", 1); err != nil {
		return fmt.Errorf("background schema: %w", err)
	}
	if len(r.handlers) > 0 {
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		c, err := rocketmq.NewPushConsumer(consumer.WithNameServer(primitive.NamesrvAddr(names)), consumer.WithGroupName(r.namespace+GroupSuffix), consumer.WithInstance(r.namespace+"_"+hex.EncodeToString(nonce[:])), consumer.WithCredentials(credentials), consumer.WithConsumeFromWhere(consumer.ConsumeFromFirstOffset), consumer.WithConsumeGoroutineNums(r.store.config.Workers), consumer.WithConsumeMessageBatchMaxSize(1), consumer.WithMaxReconsumeTimes(MaxReconsumeTimes))
		if err != nil {
			return err
		}
		if err = c.Subscribe(r.namespace+TopicSuffix, consumer.MessageSelector{}, r.receive); err != nil {
			return errors.Join(err, c.Shutdown())
		}
		if err = c.Start(); err != nil {
			return errors.Join(err, c.Shutdown())
		}
		r.consumer = c
	}
	r.started = true
	for i := 0; i < r.store.config.Publishers; i++ {
		r.wg.Add(1)
		go r.publishLoop()
	}
	if len(r.handlers) > 0 {
		for i := 0; i < r.store.config.Workers; i++ {
			r.wg.Add(1)
			go r.taskLoop(i)
		}
	}
	go func() {
		<-r.stop
		if r.consumer != nil {
			r.closeErr = r.consumer.Shutdown()
		}
		r.wg.Wait()
		r.cancel()
		close(r.done)
	}()
	return nil
}

// begin serializes admission with Stop so no WaitGroup additions race a drain.
func (r *Runtime) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}
	r.wg.Add(1)
	return true
}
func (r *Runtime) receive(_ context.Context, messages ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
	if !r.begin() {
		return consumer.ConsumeRetryLater, nil
	}
	defer r.wg.Done()
	ctx, cancel := context.WithTimeout(r.ctx, r.store.config.OperationTimeout)
	defer cancel()
	for _, msg := range messages {
		id, err := DecodeEnvelope(msg.Body, r.codec)
		if err == nil {
			err = r.store.Receive(ctx, id, r.handlers)
		}
		if err != nil {
			logx.WithContext(ctx).Errorf("background reception failed msg=%s reconsume=%d: %v", msg.MsgId, msg.ReconsumeTimes, err)
			return consumer.ConsumeRetryLater, err
		}
	}
	return consumer.ConsumeSuccess, nil
}

func (r *Runtime) Stop() {
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

func (r *Runtime) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.closeErr
	case <-ctx.Done():
		r.cancel()
		return ctx.Err()
	}
}

func (r *Runtime) pause() bool {
	timer := time.NewTimer(r.store.config.PollInterval)
	defer timer.Stop()
	select {
	case <-r.stop:
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runtime) publishLoop() {
	defer r.wg.Done()
	for {
		if !r.begin() {
			return
		}
		ctx, cancel := context.WithTimeout(r.ctx, r.store.config.OperationTimeout)
		work, err := r.store.PublishOne(ctx, r.sender, r.codec, r.namespace+TopicSuffix)
		cancel()
		r.wg.Done()
		if err != nil {
			logx.Errorf("background publish: %v", err)
		}
		if (!work || err != nil) && !r.pause() {
			return
		}
	}
}

func (r *Runtime) taskLoop(offset int) {
	defer r.wg.Done()
	for {
		if !r.begin() {
			return
		}
		h := r.handlers[offset%len(r.handlers)]
		offset++
		ctx, cancel := context.WithTimeout(r.ctx, r.store.config.OperationTimeout)
		claim, err := r.store.Claim(ctx, Tasks, h.Name)
		if err == nil && claim != nil {
			err = r.store.Execute(ctx, *claim, h)
		}
		cancel()
		r.wg.Done()
		if err != nil {
			logx.Errorf("background handler %s claim=%+v: %v", h.Name, claim, err)
		}
		if (claim == nil || err != nil) && !r.pause() {
			return
		}
	}
}
