//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	"github.com/redis/go-redis/v9"
)

func TestAuthRedisLimits(t *testing.T) {
	if err := testenv.CheckLocalDependencies(testenv.RedisAddress, []string{testenv.NameServer}, testenv.BrokerAddress); err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD"), ContextTimeoutEnabled: true})
	defer client.Close()
	cfg := authlogic.DefaultConfig()
	cfg.SendPerEmail = 2
	cfg.SendPerSource = 4
	namespace := testRun.Name("redis", "limits")
	limiter := authlogic.NewRedisLimiter(client, namespace, cfg)
	for _, v := range []struct{ scope, value string }{{"source", "127.0.0.1"}, {"subject", "a@example.com"}} {
		hash := sha256.Sum256([]byte(v.value))
		key := "rate:" + namespace + ":auth:send:" + v.scope + ":" + hex.EncodeToString(hash[:])
		if err := testRun.Record("redis", key); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := limiter.Allow(ctx, authlogic.RateSend, "a@example.com", "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	// Reconstructing the limiter (or a fresh challenge) must not reset counters.
	again := authlogic.NewRedisLimiter(client, namespace, cfg)
	if err := again.Allow(ctx, authlogic.RateSend, "a@example.com", "127.0.0.1"); !errors.Is(err, authlogic.ErrRateLimited) {
		t.Fatalf("rate reset: %v", err)
	}
	client.Close()
	if err := again.Allow(ctx, authlogic.RateSend, "a@example.com", "127.0.0.1"); err == nil || errors.Is(err, authlogic.ErrRateLimited) {
		t.Fatalf("Redis failure bypassed or hidden: %v", err)
	}
}
