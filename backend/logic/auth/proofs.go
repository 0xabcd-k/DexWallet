package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	proofKeyPrefix      = ":proof:"
	cooldownSuffix      = ":cooldown"
	telegramProofPrefix = ":telegram_proof:"
)

type Challenge struct {
	Token     string
	ExpiresAt time.Time
}

// Proofs owns only disposable authentication data. No SQL receipts or delivery states.
type Proofs struct {
	client            *redis.Client
	namespace, secret string
}

func NewProofs(client *redis.Client, namespace, secret string) *Proofs {
	return &Proofs{client, namespace, secret}
}
func (p *Proofs) Key(realm, email string) string {
	sum := sha256.Sum256([]byte(realm + ":" + email))
	return fmt.Sprintf("%s%s%x", p.namespace, proofKeyPrefix, sum)
}
func (p *Proofs) digest(parts ...string) string {
	raw, _ := json.Marshal(parts)
	h := hmac.New(sha256.New, []byte(p.secret))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func (p *Proofs) Send(ctx context.Context, realm, email, binding, locale string, mail Mailer) (*Challenge, error) {
	if mail == nil {
		return nil, ErrUnavailable
	}
	key := p.Key(realm, email)
	allowed, err := p.client.SetNX(ctx, key+cooldownSuffix, 1, ResendInterval).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !allowed {
		return nil, ErrRateLimited
	}
	var bytes [32]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(bytes[:])
	n, err := rand.Int(rand.Reader, big.NewInt(CodeSpace))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	if err = mail.SendCode(ctx, email, code, locale); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	expires := time.Now().Add(CodeLifetime)
	_, err = p.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, "token", token, "digest", p.digest(realm, email, binding, token, code), "attempts", 0)
		pipe.Expire(ctx, key, CodeLifetime)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return &Challenge{Token: token, ExpiresAt: expires}, nil
}

// Compare, count failures and consume atomically, so concurrent verification has one winner.
var consumeCode = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'token') ~= ARGV[1] then return 0 end
if tonumber(redis.call('HGET', KEYS[1], 'attempts') or '0') >= tonumber(ARGV[3]) then return 0 end
if redis.call('HGET', KEYS[1], 'digest') ~= ARGV[2] then
 redis.call('HINCRBY', KEYS[1], 'attempts', 1)
 return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

func (p *Proofs) Verify(ctx context.Context, realm, email, binding, token, code string) error {
	if !validToken(token) || len(code) != 6 {
		return ErrCode
	}
	ok, err := consumeCode.Run(ctx, p.client, []string{p.Key(realm, email)}, token, p.digest(realm, email, binding, token, code), MaxCodeAttempts).Int()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if ok != 1 {
		return ErrCode
	}
	return nil
}
func (p *Proofs) TelegramKey(hash string) string { return p.namespace + telegramProofPrefix + hash }
func (p *Proofs) UseTelegram(ctx context.Context, hash string) error {
	ok, err := p.client.SetNX(ctx, p.TelegramKey(hash), 1, TelegramLifetime+FutureTolerance).Result()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !ok {
		return ErrReplay
	}
	return nil
}
