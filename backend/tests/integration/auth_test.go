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

	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	authmodel "github.com/0xabcd-k/DexWallet/backend/model/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	initdata "github.com/telegram-mini-apps/init-data-golang"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
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

func authRequest(i int) string { return fmt.Sprintf("%064x", i) }
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

func TestAuthTelegramDevelopmentReuse(t *testing.T) {
	db, users := userDatabase(t, "telegram_development")
	conn := sqlx.NewSqlConnFromDB(db)
	cfg := authlogic.DefaultConfig()
	cfg.Secret, cfg.BotToken = authTestSecret, authTestBot
	cfg.TelegramEnabled, cfg.TelegramDevelopment = true, true
	auth, err := authlogic.New(cfg, users, authmodel.NewEmailChallengeModel(conn), authmodel.NewAuthOperationModel(conn), transaction.New(db), nil, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := authlogic.Intent{Purpose: userlogic.Login}
	raw := authTGAt(900000001, 1, time.Now().AddDate(-10, 0, 0))
	first, err := auth.VerifyTelegram(ctx, authRequest(90), raw, "127.0.0.1", login)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE auth_operation SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 YEAR")
	for _, request := range []string{authRequest(90), authRequest(91)} {
		result, err := auth.VerifyTelegram(ctx, request, raw, "127.0.0.1", login)
		if err != nil || result.User.Id != first.User.Id {
			t.Fatalf("reusable development login failed: result=%+v err=%v", result, err)
		}
	}
	mustExec(t, db, "UPDATE user SET status='disabled', auth_version=auth_version+1")
	if _, err = auth.VerifyTelegram(ctx, authRequest(92), raw, "127.0.0.1", login); !errors.Is(err, userlogic.ErrDisabled) {
		t.Fatal("development login ignored account status", err)
	}
}
func TestAuthEmailAndBinding(t *testing.T) {
	db, users := userDatabase(t, "auth_flow")
	conn := sqlx.NewSqlConnFromDB(db)
	tx := transaction.New(db)
	delivery := &fakeAuthMail{codes: map[string]string{}}
	cfg := authlogic.DefaultConfig()
	cfg.Secret = authTestSecret
	cfg.BotToken = authTestBot
	cfg.EmailEnabled = true
	cfg.TelegramEnabled = true
	auth, err := authlogic.New(cfg, users, authmodel.NewEmailChallengeModel(conn), authmodel.NewAuthOperationModel(conn), tx, delivery, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := authlogic.Intent{Purpose: userlogic.Login}
	send := func(address string, intent authlogic.Intent) *authlogic.Challenge {
		t.Helper()
		c, e := auth.SendEmail(ctx, address, "127.0.0.1", "en", intent)
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	challenge := send("Alice@EXAMPLE.COM", login)
	if _, err = auth.SendEmail(ctx, "Alice@example.com", "127.0.0.1", "en", login); !errors.Is(err, authlogic.ErrRateLimited) {
		t.Fatalf("cooldown: %v", err)
	}
	wrong := "000000"
	if delivery.code("Alice@example.com") == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if _, err = auth.VerifyEmail(ctx, authRequest(i+1), "Alice@example.com", challenge.Token, wrong, "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
			t.Fatal(err)
		}
	}
	if _, err = auth.VerifyEmail(ctx, authRequest(7), "Alice@example.com", challenge.Token, delivery.code("Alice@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("exhausted challenge accepted", err)
	}
	mustExec(t, db, "UPDATE email_challenge SET issued_at=issued_at-INTERVAL 61 SECOND")
	next := send("Alice@example.com", login)
	if _, err = auth.VerifyEmail(ctx, authRequest(8), "Alice@example.com", challenge.Token, delivery.code("Alice@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("old challenge accepted", err)
	}
	first, err := auth.VerifyEmail(ctx, authRequest(9), "Alice@example.com", next.Token, delivery.code("Alice@example.com"), "127.0.0.1", login)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := auth.VerifyEmail(ctx, authRequest(9), "Alice@example.com", next.Token, delivery.code("Alice@example.com"), "127.0.0.1", login)
	if err != nil || repeat.User.Id != first.User.Id || repeat.OperationID != first.OperationID || !repeat.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("receipt: %+v %v", repeat, err)
	}
	if _, err = auth.VerifyEmail(ctx, authRequest(10), "Alice@example.com", next.Token, delivery.code("Alice@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("consumed challenge accepted", err)
	}
	actor := userlogic.Actor{UserID: first.User.Id, AuthVersion: first.User.AuthVersion}
	if _, err = auth.Unbind(ctx, authRequest(11), actor, usermodel.IdentityEmail); !errors.Is(err, userlogic.ErrNeedSecondIdentity) {
		t.Fatal("last identity removed", err)
	}
	replacement := send("new@example.com", authlogic.Intent{Purpose: userlogic.Replace, Actor: actor})
	if _, err = auth.VerifyEmail(ctx, authRequest(70), "new@example.com", replacement.Token, delivery.code("new@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("cross-purpose challenge accepted", err)
	}
	if _, err = auth.VerifyEmail(ctx, authRequest(12), "new@example.com", replacement.Token, delivery.code("new@example.com"), "127.0.0.1", authlogic.Intent{Purpose: userlogic.Replace, Actor: actor}); !errors.Is(err, userlogic.ErrNeedSecondIdentity) {
		t.Fatal("replaced only identity", err)
	}
	// Failure rolls back consumption: the same challenge works after adding TG.
	bound, err := auth.VerifyTelegram(ctx, authRequest(13), authTG(100, 1), "127.0.0.1", authlogic.Intent{Purpose: userlogic.Bind, Actor: actor})
	if err != nil || bound.User.AuthVersion != actor.AuthVersion {
		t.Fatalf("add TG: %+v %v", bound, err)
	}
	replaced, err := auth.VerifyEmail(ctx, authRequest(12), "new@example.com", replacement.Token, delivery.code("new@example.com"), "127.0.0.1", authlogic.Intent{Purpose: userlogic.Replace, Actor: actor})
	if err != nil || replaced.User.AuthVersion != actor.AuthVersion+1 {
		t.Fatalf("replace: %+v %v", replaced, err)
	}
	if _, err = auth.Unbind(ctx, authRequest(14), actor, usermodel.IdentityEmail); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal("stale session accepted", err)
	}
	recovered, err := auth.VerifyEmail(ctx, authRequest(12), "new@example.com", replacement.Token, delivery.code("new@example.com"), "127.0.0.1", authlogic.Intent{Purpose: userlogic.Replace, Actor: actor})
	if err != nil || recovered.User.AuthVersion != replaced.User.AuthVersion {
		t.Fatal("replace receipt failed", err)
	}
	other, err := auth.VerifyTelegram(ctx, authRequest(15), authTG(200, 2), "127.0.0.1", login)
	if err != nil {
		t.Fatal(err)
	}
	actor.AuthVersion = replaced.User.AuthVersion
	if _, err = auth.VerifyTelegram(ctx, authRequest(16), authTG(200, 3), "127.0.0.1", authlogic.Intent{Purpose: userlogic.Replace, Actor: actor}); !errors.Is(err, userlogic.ErrOccupied) {
		t.Fatal("identity stolen", err)
	}
	check, err := users.FindCurrentIdentities(ctx, other.User.Id)
	if err != nil || check.Telegram.TelegramUserId != 200 {
		t.Fatal("other account changed", err)
	}
	unbound, err := auth.Unbind(ctx, authRequest(17), actor, usermodel.IdentityTelegram)
	if err != nil || unbound.User.AuthVersion != actor.AuthVersion+1 {
		t.Fatal("unbind", err)
	}
	if _, err = auth.Unbind(ctx, authRequest(17), actor, usermodel.IdentityTelegram); err != nil {
		t.Fatal("unbind receipt", err)
	}
	mustExec(t, db, "UPDATE `user` SET status='disabled' WHERE id=?", first.User.Id)
	if _, err = auth.Unbind(ctx, authRequest(17), actor, usermodel.IdentityTelegram); !errors.Is(err, userlogic.ErrDisabled) {
		t.Fatal("disabled receipt accepted", err)
	}
	var attempts int
	var digest string
	if err = db.QueryRow("SELECT attempts,code_digest FROM email_challenge WHERE email_address='Alice@example.com'").Scan(&attempts, &digest); err != nil || len(digest) != 64 || digest == delivery.code("Alice@example.com") {
		t.Fatal("plaintext code stored", err)
	}
}
func TestAuthConcurrencyAndReplay(t *testing.T) {
	db, users := userDatabase(t, "auth_concurrent")
	conn := sqlx.NewSqlConnFromDB(db)
	delivery := &fakeAuthMail{codes: map[string]string{}}
	cfg := authlogic.DefaultConfig()
	cfg.Secret = authTestSecret
	cfg.BotToken = authTestBot
	cfg.EmailEnabled = true
	cfg.TelegramEnabled = true
	auth, err := authlogic.New(cfg, users, authmodel.NewEmailChallengeModel(conn), authmodel.NewAuthOperationModel(conn), transaction.New(db), delivery, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := authlogic.Intent{Purpose: userlogic.Login}
	raw := authTG(300, 10)
	var wg sync.WaitGroup
	results := make(chan *authlogic.Result, 8)
	failures := make(chan error, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, e := auth.VerifyTelegram(ctx, authRequest(30), raw, "127.0.0.1", login)
			if e != nil {
				failures <- e
			} else {
				results <- r
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	var id int64
	for r := range results {
		if id == 0 {
			id = r.User.Id
		}
		if r.User.Id != id {
			t.Fatal("duplicate registration")
		}
	}
	if _, err = auth.VerifyTelegram(ctx, authRequest(31), raw, "127.0.0.1", login); !errors.Is(err, authlogic.ErrReplay) {
		t.Fatal("TG replay", err)
	}
	if _, err = auth.VerifyTelegram(ctx, authRequest(30), authTG(301, 11), "127.0.0.1", login); !errors.Is(err, authlogic.ErrRequestConflict) {
		t.Fatal("request reused", err)
	}
	challenge, err := auth.SendEmail(ctx, "parallel@example.com", "127.0.0.1", "en", login)
	if err != nil {
		t.Fatal(err)
	}
	success := make(chan bool, 8)
	start = make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, e := auth.VerifyEmail(ctx, authRequest(i+40), "parallel@example.com", challenge.Token, delivery.code("parallel@example.com"), "127.0.0.1", login)
			if e != nil && !errors.Is(e, authlogic.ErrCode) {
				t.Error(e)
			}
			success <- e == nil
		}(i)
	}
	close(start)
	wg.Wait()
	close(success)
	total := 0
	for ok := range success {
		if ok {
			total++
		}
	}
	if total != 1 {
		t.Fatalf("consumed %d times", total)
	}
	expired, err := auth.SendEmail(ctx, "expired@example.com", "127.0.0.1", "en", login)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE email_challenge SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE email_address='expired@example.com'")
	if _, err = auth.VerifyEmail(ctx, authRequest(60), "expired@example.com", expired.Token, delivery.code("expired@example.com"), "127.0.0.1", login); !errors.Is(err, authlogic.ErrCode) {
		t.Fatal("expired challenge accepted", err)
	}
	mustExec(t, db, "UPDATE auth_operation SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND")
	if _, err = auth.VerifyTelegram(ctx, authRequest(30), raw, "127.0.0.1", login); !errors.Is(err, authlogic.ErrExpired) {
		t.Fatal("expired receipt accepted", err)
	}
	delivery.failure = errors.New("SMTP timeout")
	if _, err = auth.SendEmail(ctx, "failed@example.com", "127.0.0.1", "en", login); !errors.Is(err, delivery.failure) {
		t.Fatal("mail failure hidden", err)
	}
	var status string
	if err = db.QueryRow("SELECT status FROM email_challenge WHERE email_address='failed@example.com'").Scan(&status); err != nil || status != "failed" {
		t.Fatal("failed delivery active", err)
	}
}
