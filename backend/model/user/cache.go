package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
)

const UserCacheTTL = 30 * time.Second
const cacheInvalidationTimeout = time.Second

var ErrCacheUnavailable = errors.New("user cache unavailable")

type userCacheRedis interface {
	Get(context.Context, string) *redis.StringCmd
	Set(context.Context, string, any, time.Duration) *redis.StatusCmd
	Del(context.Context, ...string) *redis.IntCmd
}

// UserCache shares the application's Redis client, DB and lifecycle. Only
// committed, ordinary primary-key reads enter this cache, never transaction reads.
type UserCache struct {
	client userCacheRedis
	prefix string
	flight syncx.SingleFlight
	now    func() time.Time
}

type cachedUser struct {
	User      User
	ExpiresAt time.Time
}

func NewUserCache(client userCacheRedis, namespace string) *UserCache {
	return &UserCache{client: client, prefix: namespace + ":user:", flight: syncx.NewSingleFlight(), now: time.Now}
}

func (c *UserCache) Key(id int64) string { return c.prefix + strconv.FormatInt(id, 10) }

func (c *UserCache) find(ctx context.Context, id int64, query func(context.Context, int64) (*User, error)) (*User, error) {
	key := c.Key(id)
	value, err := c.flight.Do(key, func() (any, error) {
		raw, err := c.client.Get(ctx, key).Bytes()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("%w: read user %d: %w", ErrCacheUnavailable, id, err)
		}
		var entry cachedUser
		if err == nil {
			if err = json.Unmarshal(raw, &entry); err != nil {
				return nil, fmt.Errorf("%w: decode user %d: %w", ErrCacheUnavailable, id, err)
			}
			if entry.User.Id != id || entry.ExpiresAt.IsZero() {
				return nil, fmt.Errorf("%w: invalid cached user %d", ErrCacheUnavailable, id)
			}
			if c.now().Before(entry.ExpiresAt) {
				return entry, nil
			}
		}
		// Anchor freshness BEFORE the SQL read. A slow stale read that fills after
		// a concurrent commit/delete must not start a new 30-second stale window.
		expires := c.now().Add(UserCacheTTL)
		u, err := query(ctx, id)
		if err != nil {
			return nil, err // Missing rows and DB errors are never cached as users.
		}
		entry = cachedUser{User: *u, ExpiresAt: expires}
		raw, err = json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: encode user %d: %w", ErrCacheUnavailable, id, err)
		}
		ttl := expires.Sub(c.now())
		if ttl <= 0 {
			return nil, fmt.Errorf("%w: user %d read exceeded cache freshness", ErrCacheUnavailable, id)
		}
		if err = c.client.Set(ctx, key, raw, ttl).Err(); err != nil {
			return nil, fmt.Errorf("%w: fill user %d: %w", ErrCacheUnavailable, id, err)
		}
		return entry, nil
	})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	entry := value.(cachedUser)
	if !c.now().Before(entry.ExpiresAt) {
		return nil, fmt.Errorf("%w: user %d result expired", ErrCacheUnavailable, id)
	}
	return &entry.User, nil // Each caller gets its own copy of the shared result.
}

func (c *UserCache) invalidate(ctx context.Context, id int64) {
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheInvalidationTimeout)
	defer cancel()
	if err := c.client.Del(call, c.Key(id)).Err(); err != nil {
		// The SQL write is already committed. Do not report it as rolled back or
		// retry the business operation; the fixed freshness limit is the fallback.
		logx.WithContext(ctx).Errorw("user cache invalidation failed", logx.Field("userId", id), logx.Field("cause", err))
	}
}
