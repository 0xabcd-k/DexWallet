package user

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Retains bytes past TTL deliberately: the snapshot deadline must protect
// callers even when an old Redis value is restored or a late read refills it.
type fakeUserRedis struct {
	mu                           sync.Mutex
	data                         map[string]string
	ttl                          time.Duration
	sets                         int
	readErr, writeErr, deleteErr error
}

func (f *fakeUserRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return redis.NewStringResult("", f.readErr)
	}
	value, ok := f.data[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}
func (f *fakeUserRedis) Set(_ context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return redis.NewStatusResult("", f.writeErr)
	}
	if f.data == nil {
		f.data = make(map[string]string)
	}
	f.data[key] = string(value.([]byte))
	f.ttl = ttl
	f.sets++
	return redis.NewStatusResult("OK", nil)
}
func (f *fakeUserRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return redis.NewIntResult(0, f.deleteErr)
	}
	for _, key := range keys {
		delete(f.data, key)
	}
	return redis.NewIntResult(int64(len(keys)), nil)
}

func TestUserCacheFixedFreshnessAndInvalidationFailure(t *testing.T) {
	ctx := context.Background()
	r := &fakeUserRedis{}
	c := NewUserCache(r, "test")
	now := time.Unix(1800000000, 0)
	c.now = func() time.Time { return now }
	calls := 0
	u := User{Id: 1, Status: StatusActive, AuthVersion: 1}
	query := func(context.Context, int64) (*User, error) { calls++; copy := u; return &copy, nil }
	first, err := c.find(ctx, 1, query)
	if err != nil || r.ttl != UserCacheTTL {
		t.Fatalf("first fill: %v TTL=%v", err, r.ttl)
	}
	first.Status = "caller mutation"
	u.Status = StatusDisabled
	r.deleteErr = errors.New("delete unavailable")
	c.invalidate(ctx, 1)
	for i := 0; i < 29; i++ {
		now = now.Add(time.Second)
		got, err := c.find(ctx, 1, query)
		if err != nil || got.Status != StatusActive {
			t.Fatalf("cache hit: %+v %v", got, err)
		}
	}
	if calls != 1 || r.sets != 1 {
		t.Fatalf("hits extended cache or queried DB: %d/%d", calls, r.sets)
	}
	now = now.Add(time.Second)
	got, err := c.find(ctx, 1, query)
	if err != nil || got.Status != StatusDisabled || calls != 2 {
		t.Fatalf("expiry: %+v %v calls=%d", got, err, calls)
	}
}

func TestUserCacheSlowFillDoesNotRestartFreshness(t *testing.T) {
	r := &fakeUserRedis{}
	c := NewUserCache(r, "test")
	now := time.Unix(1800000000, 0)
	c.now = func() time.Time { return now }
	calls := 0
	query := func(context.Context, int64) (*User, error) {
		calls++
		if calls == 1 {
			now = now.Add(29 * time.Second)
		}
		return &User{Id: 1, AuthVersion: int64(calls)}, nil
	}
	if _, err := c.find(context.Background(), 1, query); err != nil || r.ttl != time.Second {
		t.Fatalf("slow fill: %v %v", err, r.ttl)
	}
	now = now.Add(time.Second)
	got, err := c.find(context.Background(), 1, query)
	if err != nil || got.AuthVersion != 2 {
		t.Fatalf("stale fill extended freshness: %+v %v", got, err)
	}
}

func TestUserCacheErrorsDoNotBecomeMisses(t *testing.T) {
	for _, failure := range []string{"read", "write", "corrupt", "database", "missing"} {
		t.Run(failure, func(t *testing.T) {
			r := &fakeUserRedis{}
			c := NewUserCache(r, "test")
			cause := errors.New("unavailable")
			switch failure {
			case "read":
				r.readErr = cause
			case "write":
				r.writeErr = cause
			case "corrupt":
				r.data = map[string]string{c.Key(1): "bad JSON"}
			}
			calls := 0
			_, err := c.find(context.Background(), 1, func(context.Context, int64) (*User, error) {
				calls++
				if failure == "database" {
					return nil, cause
				}
				if failure == "missing" {
					return nil, ErrNotFound
				}
				return &User{Id: 1}, nil
			})
			if err == nil {
				t.Fatal("failure hidden")
			}
			if (failure == "read" || failure == "corrupt") && calls != 0 {
				t.Fatal("cache failure fell through to database")
			}
			if failure == "missing" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			} else if failure == "database" {
				if !errors.Is(err, cause) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrCacheUnavailable) {
				t.Fatal(err)
			}
			if len(r.data) != 0 && failure != "corrupt" {
				t.Fatal("failed result cached")
			}
		})
	}
}

func TestUserCacheCoalescesConcurrentReads(t *testing.T) {
	c := NewUserCache(&fakeUserRedis{}, "test")
	var calls atomic.Int32
	start, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			u, err := c.find(context.Background(), 1, func(context.Context, int64) (*User, error) {
				calls.Add(1)
				<-release
				return &User{Id: 1, Nickname: "original"}, nil
			})
			if err != nil || u.Nickname != "original" {
				t.Errorf("concurrent read: %+v %v", u, err)
				return
			}
			u.Nickname = "caller change"
		}()
	}
	close(start)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("queries=%d", calls.Load())
	}
}

func TestCachedTransactionWriteRequiresRunnerContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewUserModelWithCache(sqlx.NewSqlConnFromDB(db), NewUserCache(&fakeUserRedis{}, "test"))
	if err = m.WithSession(sqlx.NewSessionFromTx(nil)).Update(context.Background(), &User{Id: 1}); err == nil {
		t.Fatal("transaction cache invalidation silently omitted")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCachedMutationCommitAndRollback(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			r := &fakeUserRedis{data: map[string]string{"test:user:1": "old"}}
			m := NewUserModelWithCache(sqlx.NewSqlConnFromDB(db), NewUserCache(r, "test"))
			mock.ExpectBegin()
			mock.ExpectExec("update `user`").WillReturnResult(sqlmock.NewResult(0, 1))
			if commit {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			rejected := errors.New("rejected")
			err = transaction.New(db).TransactCtx(context.Background(), func(ctx context.Context, session sqlx.Session) error {
				if err := m.WithSession(session).Update(ctx, &User{Id: 1}); err != nil {
					return err
				}
				if _, ok := r.data["test:user:1"]; !ok {
					t.Fatal("cache deleted before commit")
				}
				if !commit {
					return rejected
				}
				return nil
			})
			if commit && err != nil || !commit && !errors.Is(err, rejected) {
				t.Fatal(err)
			}
			_, exists := r.data["test:user:1"]
			if exists == commit {
				t.Fatalf("cache state after commit=%v: exists=%v", commit, exists)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
