package user

import (
	"context"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/readcache"
)

const UserCacheTTL = readcache.TTL

type UserCache struct {
	cache  *readcache.Cache[User]
	prefix string
}

func NewUserCache(client readcache.Client, namespace string) *UserCache {
	return &UserCache{cache: readcache.New[User](client), prefix: namespace + ":user:"}
}
func (c *UserCache) Key(id int64) string { return c.prefix + strconv.FormatInt(id, 10) }
func (c *UserCache) find(ctx context.Context, id int64, query func(context.Context, int64) (*User, error)) (*User, error) {
	return c.cache.Get(ctx, c.Key(id), func() (*User, error) { return query(ctx, id) })
}
func (c *UserCache) invalidate(ctx context.Context, id int64) { c.cache.Delete(ctx, c.Key(id)) }
