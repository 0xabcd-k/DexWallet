//go:build integration

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
	initdata "github.com/telegram-mini-apps/init-data-golang"
	"github.com/zeromicro/go-zero/rest"
)

const sessionTestSecret = "test-only-session-signing-secret-32"
const sessionTestBot = "123456:integration-fake-token"

type sessionMail struct {
	mu    sync.Mutex
	codes map[string]string
}

func (m *sessionMail) SendCode(_ context.Context, address, code, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[address] = code
	return nil
}
func (m *sessionMail) code(address string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[address]
}

type sessionLimit struct{}

func (sessionLimit) Allow(context.Context, authlogic.RateAction, string, string) error { return nil }

func TestProductSessionHTTP(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "session_http")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrationRun, err := migration.Open(dsn, name, "../../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrationRun.Up(); err != nil {
		t.Fatal(err)
	}
	if a, b := migrationRun.Close(); a != nil || b != nil {
		t.Fatal(a, b)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.MultiStatements = false
	t.Setenv("DEXWALLET_MYSQL_DSN", parsed.FormatDSN())
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	c, err := config.Load("../../etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Environment = "integration_" + testRun.ID
	c.RocketMQ.Enabled = false
	c.Redis.Address = testenv.RedisAddress
	cfg := authlogic.DefaultConfig()
	cfg.Secret = "test-only-auth-operation-secret-32"
	cfg.TelegramEnabled = true
	cfg.BotToken = sessionTestBot
	c.Auth = &cfg
	c.Session = &session.Config{Secret: sessionTestSecret}
	s, err := shared.NewServiceContext(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
			t.Error(err)
		}
	}()
	mail := &sessionMail{codes: map[string]string{}}
	cfg.EmailEnabled = true
	proofs := authlogic.NewProofs(s.Redis, s.Namespace, cfg.Secret)
	s.Auth, err = authlogic.New(cfg, s.Users, proofs, mail, sessionLimit{})
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := Build(s)
	if err != nil {
		t.Fatal(err)
	}
	rt.Report = func(context.Context, httpapi.Event) {}
	adapter, err := rest.NewServerless(app)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(rt.Middleware(http.HandlerFunc(adapter.Serve)))
	defer ts.Close()
	defer func() {
		rt.Stop()
		drain, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := rt.Wait(drain); err != nil {
			t.Error(err)
		}
	}()
	call := func(method, path, token string, input any, wantStatus int, wantCode string) (http.Header, json.RawMessage) {
		t.Helper()
		var body []byte
		if input != nil {
			body, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if wantStatus == http.StatusNotFound {
			if res.StatusCode != wantStatus || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") || len(res.Header.Get(httpapi.TraceHeader)) != 32 || res.Header.Get(session.TokenHeader) != "" {
				t.Fatal("fallback HTTP contract", res.Status, res.Header)
			}
			return res.Header, nil
		}
		var envelope struct {
			Code    string          `json:"code"`
			Data    json.RawMessage `json:"data"`
			TraceId string          `json:"traceId"`
		}
		if err = json.NewDecoder(res.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != wantStatus || envelope.Code != wantCode {
			t.Fatalf("%s %s: status %d code %s, want %d %s", method, path, res.StatusCode, envelope.Code, wantStatus, wantCode)
		}
		if envelope.TraceId != res.Header.Get(httpapi.TraceHeader) || len(envelope.TraceId) != 32 {
			t.Fatal("trace contract lost")
		}
		// Removed routes use the common 404 handler, outside AuthTransport.
		if wantStatus != http.StatusNotFound && res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("authentication response cacheable")
		}
		if wantStatus != 200 && res.Header.Get(session.TokenHeader) != "" {
			t.Fatal("error renewed token")
		}
		return res.Header, envelope.Data
	}
	decodeToken := func(h http.Header, b json.RawMessage) string {
		t.Helper()
		var v types.TokenData
		if err = json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		claims, id, e := s.Tokens.Parse(v.AccessToken)
		if e != nil || v.AccessToken != h.Get(session.TokenHeader) || claims.Subject != v.UserIdx || claims.ExpiresAt.Unix() != v.ExpiresAt {
			t.Fatal("token response mismatch", e)
		}
		if e = testRun.Record("redis", s.RedisKey("user", strconv.FormatInt(id, 10))); e != nil {
			t.Fatal(e)
		}
		return v.AccessToken
	}
	send := func(email, token, purpose string) string {
		t.Helper()
		normalized, _ := userlogic.NormalizeEmail(email)
		for _, key := range []string{proofs.Key("user", normalized.Address), proofs.Key("user", normalized.Address) + ":cooldown"} {
			if err := testRun.Record("redis", key); err != nil {
				t.Fatal(err)
			}
		}
		path := "/v1/user/auth/email/code"
		input := map[string]string{"email": email, "locale": "en"}
		if purpose != "" {
			path = "/v1/user/identities/email/code"
			input["purpose"] = purpose
		}
		_, data := call("POST", path, token, input, 200, "OK")
		var result types.ChallengeData
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result.Challenge
	}
	tg := func(id int64, nonce int) string {
		now := time.Now()
		p := map[string]string{"user": fmt.Sprintf(`{"id":%d,"first_name":"Test User"}`, id), "query_id": strconv.Itoa(nonce)}
		q := url.Values{}
		for k, v := range p {
			q.Set(k, v)
		}
		q.Set("auth_date", strconv.FormatInt(now.Unix(), 10))
		q.Set("hash", initdata.Sign(p, sessionTestBot, now))
		if err := testRun.Record("redis", proofs.TelegramKey(q.Get("hash"))); err != nil {
			t.Fatal(err)
		}
		return q.Encode()
	}

	call("GET", "/v1/user/current", "", nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/user/accounts", "", nil, 401, "UNAUTHENTICATED")
	code := send("Alice@EXAMPLE.COM", "", "")
	input := map[string]string{"email": "Alice@example.com", "challenge": code, "code": mail.code("Alice@example.com")}
	wrong := map[string]string{}
	for k, v := range input {
		wrong[k] = v
	}
	wrong["code"] = "000000"
	if wrong["code"] == input["code"] {
		wrong["code"] = "111111"
	}
	call("POST", "/v1/user/auth/email/login", "", wrong, 400, "AUTH_PROOF_INVALID")
	h, b := call("POST", "/v1/user/auth/email/login", "", input, 200, "OK")
	token := decodeToken(h, b)
	claims, id, err := s.Tokens.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	h, b = call("GET", "/v1/user/current", token, nil, 200, "OK")
	var current types.CurrentUserData
	if err = json.Unmarshal(b, &current); err != nil || current.Idx != claims.Subject || h.Get(session.TokenHeader) != "" {
		t.Fatal("current user contract", err)
	}
	_, b = call("GET", "/v1/user/accounts", token, nil, 200, "OK")
	var balances types.AccountBalancesData
	if err = json.Unmarshal(b, &balances); err != nil || len(balances.Accounts) != 3 || balances.Accounts[0].Balance != "0.00" || balances.Accounts[2].Balance != "0" {
		t.Fatal("initial balances", balances, err)
	}
	var accountCount int
	if err = db.QueryRow("SELECT COUNT(*) FROM user_account").Scan(&accountCount); err != nil || accountCount != 0 {
		t.Fatal("login/query created accounts", err)
	}
	if _, err = db.Exec("INSERT INTO user_account(user_id,kind,balance,held_balance) VALUES(?,'usd_balance','9007199254740993.01','2.09')", id); err != nil {
		t.Fatal(err)
	}
	_, b = call("GET", "/v1/user/accounts?userId=999", token, nil, 200, "OK")
	if err = json.Unmarshal(b, &balances); err != nil || balances.Accounts[0].TotalBalance != "9007199254740995.10" {
		t.Fatal("exact balance or authenticated owner lost", balances, err)
	}
	_, b = call("GET", "/v1/user/identities", token, nil, 200, "OK")
	var identityList types.IdentitiesData
	if err = json.Unmarshal(b, &identityList); err != nil || len(identityList.Identities) != 1 || identityList.Identities[0].Subject != "Alice@example.com" {
		t.Fatal("current identities", err)
	}
	call("PATCH", "/v1/user/profile", token, map[string]string{"nickname": "   "}, 400, "INVALID_ARGUMENT")
	call("PATCH", "/v1/user/profile", token, map[string]string{"nickname": strings.Repeat("n", 129)}, 400, "INVALID_ARGUMENT")
	call("PATCH", "/v1/user/profile", token, map[string]string{"nickname": " Alice Product "}, 200, "OK")
	_, b = call("GET", "/v1/user/current", token, nil, 200, "OK")
	if err = json.Unmarshal(b, &current); err != nil || current.Nickname != "Alice Product" || current.Idx != claims.Subject {
		t.Fatal("profile cache invalidation", err)
	}
	oldClaims := *claims
	oldClaims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-37 * time.Hour))
	oldClaims.ExpiresAt = jwt.NewNumericDate(oldClaims.IssuedAt.Add(session.Lifetime))
	old, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &oldClaims).SignedString([]byte(sessionTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	h, _ = call("GET", "/v1/user/current", old, nil, 200, "OK")
	renewed, _, err := s.Tokens.Parse(h.Get(session.TokenHeader))
	if err != nil || renewed.AuthVersion != claims.AuthVersion || !renewed.ExpiresAt.After(oldClaims.ExpiresAt.Time) {
		t.Fatal("HTTP renewal", err)
	}

	bind := map[string]string{"purpose": "bind", "initData": tg(100, 1)}
	h, b = call("POST", "/v1/user/identities/telegram/verify", token, bind, 200, "OK")
	token = decodeToken(h, b)
	replacement := map[string]string{"purpose": "replace", "email": "new@example.com", "challenge": send("new@example.com", token, "replace"), "code": mail.code("new@example.com")}
	h, b = call("POST", "/v1/user/identities/email/verify", token, replacement, 200, "OK")
	changed := decodeToken(h, b)
	call("GET", "/v1/user/current", token, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/user/accounts", token, nil, 401, "UNAUTHENTICATED")
	_, b = call("GET", "/v1/user/accounts", changed, nil, 200, "OK")
	if err = json.Unmarshal(b, &balances); err != nil || balances.Accounts[0].Balance != "9007199254740993.01" {
		t.Fatal("identity replacement changed account", balances, err)
	}
	_, b = call("GET", "/v1/user/current", changed, nil, 200, "OK")
	if err = json.Unmarshal(b, &current); err != nil || current.Nickname != "Alice Product" {
		t.Fatal("identity change overwrote product profile", err)
	}
	// Consumed proofs and obsolete tokens have no success-recovery bypass.
	call("POST", "/v1/user/identities/email/verify", token, replacement, 401, "UNAUTHENTICATED")
	call("POST", "/v1/user/identities/unbind", token, map[string]string{"identity": "telegram"}, 401, "UNAUTHENTICATED")
	call("POST", "/v1/user/identities/email/code", token, map[string]string{"email": "stale@example.com", "purpose": "replace"}, 401, "UNAUTHENTICATED")
	replacement["email"] = "different@example.com"
	call("POST", "/v1/user/identities/email/verify", token, replacement, 401, "UNAUTHENTICATED")
	unbind := map[string]string{"identity": "telegram"}
	h, b = call("POST", "/v1/user/identities/unbind", changed, unbind, 200, "OK")
	unbound := decodeToken(h, b)
	call("POST", "/v1/user/identities/unbind", changed, unbind, 401, "UNAUTHENTICATED")
	call("POST", "/v1/user/identities/unbind", unbound, map[string]string{"identity": "email"}, 422, "ANOTHER_IDENTITY_REQUIRED")
	// The retired public endpoint neither renews nor changes the current session.
	// The retained internal capability still invalidates tokens.
	logoutClaims, _, err := s.Tokens.Parse(unbound)
	if err != nil {
		t.Fatal(err)
	}
	logoutClaims.IssuedAt = oldClaims.IssuedAt
	logoutClaims.ExpiresAt = oldClaims.ExpiresAt
	logoutToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, logoutClaims).SignedString([]byte(sessionTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	h, _ = call("POST", "/v1/user/auth/logout-all", logoutToken, nil, 404, "NOT_FOUND")
	if h.Get(session.TokenHeader) != "" {
		t.Fatal("retired endpoint renewed")
	}
	call("GET", "/v1/user/current", unbound, nil, 200, "OK")
	if err = s.Users.LogoutAll(ctx, userlogic.Actor{UserID: id, AuthVersion: logoutClaims.AuthVersion}); err != nil {
		t.Fatal("internal logout all", err)
	}
	call("GET", "/v1/user/current", unbound, nil, 401, "UNAUTHENTICATED")
	call("POST", "/v1/user/identities/unbind", changed, unbind, 401, "UNAUTHENTICATED")

	// TG-only silent login creates an independent product user.
	h, b = call("POST", "/v1/user/auth/telegram/login", "", map[string]string{"initData": tg(200, 2)}, 200, "OK")
	tgToken := decodeToken(h, b)
	tgClaims, tgID, err := s.Tokens.Parse(tgToken)
	if err != nil || tgID == id {
		t.Fatal("TG user not independent", err)
	}
	call("GET", "/v1/user/current", tgToken, nil, 200, "OK")
	_, b = call("GET", "/v1/user/accounts", tgToken, nil, 200, "OK")
	if err = json.Unmarshal(b, &balances); err != nil || balances.Accounts[0].Balance != "0.00" {
		t.Fatal("TG user saw another user's funds", balances, err)
	}
	// An external write without invalidation leaves an accepted short cache window.
	// Critical writes still reject the stale actor against the locked database row.
	if _, err = s.DB.ExecContext(ctx, "UPDATE `user` SET auth_version=auth_version+1 WHERE id=?", tgID); err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/current", tgToken, nil, 200, "OK")
	if err = s.Users.LogoutAll(ctx, userlogic.Actor{UserID: tgID, AuthVersion: tgClaims.AuthVersion}); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal("internal logout all accepted stale actor", err)
	}
	call("PATCH", "/v1/user/profile", tgToken, map[string]string{"nickname": "stale write"}, 401, "UNAUTHENTICATED")
	call("GET", "/v1/user/identities", tgToken, nil, 200, "OK")
	tgClaims.IssuedAt, tgClaims.ExpiresAt = oldClaims.IssuedAt, oldClaims.ExpiresAt
	stale, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tgClaims).SignedString([]byte(sessionTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	h, _ = call("GET", "/v1/user/current", stale, nil, 200, "OK")
	staleClaims, _, err := s.Tokens.Parse(h.Get(session.TokenHeader))
	if err != nil || staleClaims.AuthVersion != tgClaims.AuthVersion {
		t.Fatal("stale cache upgraded token version", err)
	}
	if err = s.Redis.Del(ctx, s.RedisKey("user", strconv.FormatInt(tgID, 10))).Err(); err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/current", tgToken, nil, 401, "UNAUTHENTICATED")
	if _, err = s.DB.ExecContext(ctx, "UPDATE `user` SET status='disabled' WHERE id=?", tgID); err != nil {
		t.Fatal(err)
	}
	if err = s.Redis.Del(ctx, s.RedisKey("user", strconv.FormatInt(tgID, 10))).Err(); err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/current", tgToken, nil, 403, "PERMISSION_DENIED")
	call("GET", "/v1/user/accounts", tgToken, nil, 403, "PERMISSION_DENIED")
	// Close only this test client's connection pool, never the shared Redis server.
	if err = s.Redis.Close(); err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/current", tgToken, nil, 403, "PERMISSION_DENIED")
}
