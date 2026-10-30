// Package readcache accelerates ordinary reads. It never stores authentication proofs.
package readcache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
)

const TTL = 30 * time.Second

type Client interface {
	Get(context.Context, string) *redis.StringCmd
	Set(context.Context, string, any, time.Duration) *redis.StatusCmd
	Del(context.Context, ...string) *redis.IntCmd
}
type Cache[T any] struct {
	client Client
	flight syncx.SingleFlight
}

func New[T any](client Client) *Cache[T] {
	return &Cache[T]{client: client, flight: syncx.NewSingleFlight()}
}
func (c *Cache[T]) Get(ctx context.Context, key string, query func() (*T, error)) (*T, error) {
	value, err := c.flight.Do(key, func() (any, error) {
		raw, err := c.client.Get(ctx, key).Bytes()
		var entry struct{ Value *T }
		if err == nil {
			err = json.Unmarshal(raw, &entry)
			if err == nil && entry.Value != nil {
				return *entry.Value, nil
			}
			if err == nil {
				err = errors.New("invalid cache entry")
			}
		}
		if !errors.Is(err, redis.Nil) {
			logx.WithContext(ctx).Errorw("read cache miss after failure", logx.Field("key", key), logx.Field("cause", err))
		}
		value, err := query()
		if err != nil {
			return nil, err
		}
		entry.Value = value
		raw, err = json.Marshal(entry)
		if err == nil {
			err = c.client.Set(ctx, key, raw, TTL).Err()
		}
		if err != nil {
			logx.WithContext(ctx).Errorw("read cache fill failed", logx.Field("key", key), logx.Field("cause", err))
		}
		return *value, nil
	})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	result := value.(T)
	return &result, nil
}
func (c *Cache[T]) Delete(ctx context.Context, key string) {
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := c.client.Del(call, key).Err(); err != nil {
		logx.WithContext(ctx).Errorw("read cache invalidation failed", logx.Field("key", key), logx.Field("cause", err))
	}
}
