package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

type RateAction string

const (
	RateSend     RateAction = "send"
	RateVerify   RateAction = "verify"
	RateTelegram RateAction = "telegram"
)

type RedisLimiter struct {
	limiter   *redis_rate.Limiter
	namespace string
	cfg       Config
}

func NewRedisLimiter(client *redis.Client, namespace string, cfg Config) *RedisLimiter {
	return &RedisLimiter{redis_rate.NewLimiter(client), namespace, cfg}
}
func (l *RedisLimiter) Allow(ctx context.Context, phase RateAction, subject, source string) error {
	if phase != RateSend && phase != RateVerify && phase != RateTelegram {
		return ErrInvalid
	}
	subjectRate, sourceRate := l.cfg.VerifyPerSubject, l.cfg.VerifyPerSource
	if phase == RateSend {
		subjectRate, sourceRate = l.cfg.SendPerEmail, l.cfg.SendPerSource
	}
	for _, scope := range []struct {
		name, value string
		rate        int
	}{{"source", source, sourceRate}, {"subject", subject, subjectRate}} {
		if scope.value == "" {
			continue
		}
		hash := sha256.Sum256([]byte(scope.value))
		key := l.namespace + ":auth:" + string(phase) + ":" + scope.name + ":" + hex.EncodeToString(hash[:])
		result, err := l.limiter.Allow(ctx, key, redis_rate.PerHour(scope.rate))
		if err != nil {
			return fmt.Errorf("%w: authentication rate store: %w", ErrUnavailable, err)
		}
		if result.Allowed == 0 {
			return ErrRateLimited
		}
	}
	return nil
}
