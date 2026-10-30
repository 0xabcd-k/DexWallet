//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/redis/go-redis/v9"
	initdata "github.com/telegram-mini-apps/init-data-golang"
	"os"
)

type fakeAuthMail struct {
	mu      sync.Mutex
	codes   map[string]string
	failure error
}

func (f *fakeAuthMail) SendCode(ctx context.Context, address, code, locale string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[address] = code
	return f.failure
}
func (f *fakeAuthMail) code(address string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.codes[address]
}

type allowAuth struct{}

func (allowAuth) Allow(context.Context, authlogic.RateAction, string, string) error { return nil }

const authTestSecret = "test-only-auth-secret-32-characters"
const authTestBot = "123456:integration-fake-token"

func authTG(id int64, nonce int) string {
	return authTGAt(id, nonce, time.Now())
}
func authTGAt(id int64, nonce int, now time.Time) string {
	payload := map[string]string{"user": fmt.Sprintf(`{"id":%d,"first_name":"Telegram User"}`, id), "query_id": fmt.Sprint(nonce)}
	q := url.Values{}
	for k, v := range payload {
		q.Set(k, v)
	}
	q.Set("auth_date", strconv.FormatInt(now.Unix(), 10))
	q.Set("hash", initdata.Sign(payload, authTestBot, now))
	return q.Encode()
}

func proofStore(t *testing.T, label string) (*authlogic.Proofs, *redis.Client) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD"), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { client.Close() })
	return authlogic.NewProofs(client, testRun.Name("redis", label), authTestSecret), client
}
func recordEmailProof(t *testing.T, p *authlogic.Proofs, realm, email string) {
	t.Helper()
	for _, key := range []string{p.Key(realm, email), p.Key(realm, email) + ":cooldown"} {
		if err := testRun.Record("redis", key); err != nil {
			t.Fatal(err)
		}
	}
}
func recordTelegramProof(t *testing.T, p *authlogic.Proofs, raw string) {
	t.Helper()
	q, _ := url.ParseQuery(raw)
	if err := testRun.Record("redis", p.TelegramKey(q.Get("hash"))); err != nil {
		t.Fatal(err)
	}
}
func TestAuthTelegramDevelopmentReuse(t *testing.T) {
	db, users := userDatabase(t, "telegram_development")
	p, _ := proofStore(t, "dev_proofs")
	cfg := authlogic.DefaultConfig()
	cfg.Secret, cfg.BotToken = authTestSecret, authTestBot
	cfg.TelegramEnabled, cfg.TelegramDevelopment = true, true
	auth, err := authlogic.New(cfg, users, p, nil, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := authlogic.Intent{Purpose: userlogic.Login}
	raw := authTGAt(900000001, 1, time.Now().AddDate(-10, 0, 0))
	first, err := auth.VerifyTelegram(ctx, raw, "127.0.0.1", login)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := auth.VerifyTelegram(ctx, raw, "127.0.0.1", login)
		if err != nil || result.User.Id != first.User.Id {
			t.Fatalf("development reuse: %+v %v", result, err)
		}
	}
	mustExec(t, db, "UPDATE user SET status='disabled',auth_version=auth_version+1")
	if _, err = auth.VerifyTelegram(ctx, raw, "127.0.0.1", login); !errors.Is(err, userlogic.ErrDisabled) {
		t.Fatal("development bypassed status", err)
	}
}
func TestAuthEmailAndBinding(t *testing.T) {
	_, users := userDatabase(t, "auth_flow")
	p, client := proofStore(t, "proof_flow")
	ctx := context.Background()
	delivery := &fakeAuthMail{codes: map[string]string{}}
	cfg := authlogic.DefaultConfig()
	cfg.Secret, cfg.BotToken = authTestSecret, authTestBot
	cfg.EmailEnabled, cfg.TelegramEnabled = true, true
	auth, err := authlogic.New(cfg, users, p, delivery, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	login := authlogic.Intent{Purpose: userlogic.Login}
	send := func(address string, intent authlogic.Intent) *authlogic.Challenge {
		t.Helper()
		recordEmailProof(t, p, "user", address)
		c, e := auth.SendEmail(ctx, address, "127.0.0.1", "en", intent)
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	verify := func(address string, c *authlogic.Challenge, intent authlogic.Intent) (*authlogic.Result, error) {
		return auth.VerifyEmail(ctx, address, c.Token, delivery.code(address), "127.0.0.1", intent)
	}
	resend := func(address string) {
		t.Helper()
		if err := client.Del(ctx, p.Key("user", address)+":cooldown").Err(); err != nil {
			t.Fatal(err)
		}
	}
	tg := func(id int64, nonce int, intent authlogic.Intent) (*authlogic.Result, error) {
		raw := authTG(id, nonce)
		recordTelegramProof(t, p, raw)
		return auth.VerifyTelegram(ctx, raw, "127.0.0.1", intent)
	}
	c := send("Alice@example.com", login)
	if _, err = auth.SendEmail(ctx, "Alice@EXAMPLE.COM", "127.0.0.1", "en", login); !errors.Is(err, authlogic.ErrRateLimited) {
		t.Fatal("cooldown", err)
	}
	digest, err := client.HGet(ctx, p.Key("user", "Alice@example.com"), "digest").Result()
	if err != nil || len(digest) != 64 || digest == delivery.code("Alice@example.com") {
		t.Fatal("plaintext code stored", err)
	}
	ttl, err := client.TTL(ctx, p.Key("user", "Alice@example.com")).Result()
	if err != nil || ttl < 4*time.Minute || ttl > authlogic.CodeLifetime {
		t.Fatal("proof TTL", ttl, err)
	}
	wrong := "000000"
	if wrong == delivery.code("Alice@example.com") {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if _, err = auth.VerifyEmail(ctx, "Alice@example.com", c.Token, wrong, "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
			t.Fatal(err)
		}
	}
	if _, err = verify("Alice@example.com", c, login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("exhausted proof", err)
	}
	resend("Alice@example.com")
	next := send("Alice@example.com", login)
	if _, err = verify("Alice@example.com", c, login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("old challenge", err)
	}
	first, err := verify("Alice@example.com", next, login)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verify("Alice@example.com", next, login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("consumed challenge", err)
	}
	actor := userlogic.Actor{UserID: first.User.Id, AuthVersion: first.User.AuthVersion}
	if _, err = auth.Unbind(ctx, actor, usermodel.IdentityEmail); !errors.Is(err, userlogic.ErrNeedSecondIdentity) {
		t.Fatal("last identity removed", err)
	}
	intent := authlogic.Intent{Purpose: userlogic.Replace, Actor: actor}
	c = send("new@example.com", intent)
	if _, err = verify("new@example.com", c, login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("cross-purpose proof", err)
	}
	if _, err = verify("new@example.com", c, intent); !errors.Is(err, userlogic.ErrNeedSecondIdentity) {
		t.Fatal("replaced only identity", err)
	}
	if _, err = verify("new@example.com", c, intent); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("business failure restored proof", err)
	}
	if _, err = tg(100, 1, authlogic.Intent{Purpose: userlogic.Bind, Actor: actor}); err != nil {
		t.Fatal(err)
	}
	resend("new@example.com")
	c = send("new@example.com", intent)
	replaced, err := verify("new@example.com", c, intent)
	if err != nil || replaced.User.AuthVersion != actor.AuthVersion+1 {
		t.Fatalf("replace: %+v %v", replaced, err)
	}
	if _, err = auth.Unbind(ctx, actor, usermodel.IdentityTelegram); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal("stale actor", err)
	}
	other, err := tg(200, 2, login)
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = replaced.User.AuthVersion
	if _, err = tg(200, 3, authlogic.Intent{Purpose: userlogic.Replace, Actor: actor}); !errors.Is(err, userlogic.ErrOccupied) {
		t.Fatal("identity stolen", err)
	}
	check, err := users.FindCurrentIdentities(ctx, other.User.Id)
	if err != nil || check.Telegram.TelegramUserId != 200 {
		t.Fatal("other account changed", err)
	}
	unbound, err := auth.Unbind(ctx, actor, usermodel.IdentityTelegram)
	if err != nil || unbound.User.AuthVersion != actor.AuthVersion+1 {
		t.Fatal("unbind", err)
	}
	if _, err = auth.Unbind(ctx, actor, usermodel.IdentityTelegram); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal("old actor replay", err)
	}
}
func TestAuthConcurrencyAndReplay(t *testing.T) {
	_, users := userDatabase(t, "auth_concurrent")
	p, client := proofStore(t, "proof_concurrent")
	ctx := context.Background()
	delivery := &fakeAuthMail{codes: map[string]string{}}
	cfg := authlogic.DefaultConfig()
	cfg.Secret, cfg.BotToken = authTestSecret, authTestBot
	cfg.EmailEnabled, cfg.TelegramEnabled = true, true
	auth, err := authlogic.New(cfg, users, p, delivery, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	login := authlogic.Intent{Purpose: userlogic.Login}
	raw := authTG(300, 10)
	recordTelegramProof(t, p, raw)
	concurrent := func(work func() error, rejected error) {
		t.Helper()
		var wg sync.WaitGroup
		success := make(chan bool, 8)
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := work()
				if err != nil && !errors.Is(err, rejected) {
					t.Error(err)
				}
				success <- err == nil
			}()
		}
		close(start)
		wg.Wait()
		close(success)
		count := 0
		for ok := range success {
			if ok {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("consumed %d times", count)
		}
	}
	concurrent(func() error { _, err := auth.VerifyTelegram(ctx, raw, "127.0.0.1", login); return err }, authlogic.ErrReplay)
	recordEmailProof(t, p, "user", "parallel@example.com")
	c, err := auth.SendEmail(ctx, "parallel@example.com", "127.0.0.1", "en", login)
	if err != nil {
		t.Fatal(err)
	}
	concurrent(func() error {
		_, err := auth.VerifyEmail(ctx, "parallel@example.com", c.Token, delivery.code("parallel@example.com"), "127.0.0.1", login)
		return err
	}, authlogic.ErrCode)
	recordEmailProof(t, p, "user", "expired@example.com")
	c, err = auth.SendEmail(ctx, "expired@example.com", "127.0.0.1", "en", login)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Expire(ctx, p.Key("user", "expired@example.com"), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.VerifyEmail(ctx, "expired@example.com", c.Token, delivery.code("expired@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("expired proof", err)
	}
	recordEmailProof(t, p, "user", "failed@example.com")
	delivery.failure = errors.New("SMTP timeout")
	if _, err = auth.SendEmail(ctx, "failed@example.com", "127.0.0.1", "en", login); !errors.Is(err, delivery.failure) {
		t.Fatal("mail failure hidden", err)
	}
	if count, err := client.Exists(ctx, p.Key("user", "failed@example.com")).Result(); err != nil || count != 0 {
		t.Fatal("failed delivery active", err)
	}
	client.Close()
	if err = p.Verify(ctx, "user", "parallel@example.com", "login:0:0", c.Token, "123456"); !errors.Is(err, authlogic.ErrUnavailable) {
		t.Fatal("Redis proof validation failed open", err)
	}
	if err = p.UseTelegram(ctx, "closed-client"); !errors.Is(err, authlogic.ErrUnavailable) {
		t.Fatal("Redis replay check failed open", err)
	}
}
