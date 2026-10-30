//go:build integration

package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
	shared "github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/rest"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBackofficeHTTP(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "backoffice_http")
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
	c.Backoffice = &backoffice.Config{AdminSecret: strings.Repeat("a", 32), MerchantSecret: strings.Repeat("m", 32)}
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
	conn := sqlx.NewSqlConnFromDB(s.DB)
	proofs := authlogic.NewProofs(s.Redis, s.Namespace, cfg.Secret)
	s.Auth, err = authlogic.New(cfg, s.Users, proofs, mail, sessionLimit{})
	if err != nil {
		t.Fatal(err)
	}
	s.Backoffice, err = backoffice.New(*c.Backoffice, cfg, conn, s.Tx, s.Users, s.UserIdx, s.Redis, s.Namespace, c.IDSecret, mail)
	if err != nil {
		t.Fatal(err)
	}
	// Record all possible keys before sending requests, including failed attempts.
	for _, realm := range []string{"admin", "merchant"} {
		for _, phase := range []string{"send", "verify"} {
			for scope, values := range map[string][]string{"source": {"127.0.0.1"}, "subject": {"admin@example.com", "merchant@example.com"}} {
				for _, value := range values {
					sum := sha256.Sum256([]byte(value))
					key := fmt.Sprintf("rate:%s:backoffice:%s:auth:%s:%s:%x", s.Namespace, realm, phase, scope, sum)
					if err = testRun.Record("redis", key); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		for id := 1; id <= 3; id++ {
			if err = testRun.Record("redis", fmt.Sprintf("%s:backoffice:account:%s:%d", s.Namespace, realm, id)); err != nil {
				t.Fatal(err)
			}
		}
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

	const initial = "Temporary-password-123"
	const changed = "Changed-password-456"
	super := backoffice.Actor{Realm: backoffice.Super, TraceID: "integration-super"}
	admin, err := s.Backoffice.Create(ctx, super, backoffice.Admin, "admin@example.com", initial, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	decodeToken := func(h http.Header, b json.RawMessage) string {
		t.Helper()
		var v types.TokenData
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		if h.Get(session.TokenHeader) != v.AccessToken || v.AccessToken == "" {
			t.Fatal("token header/body mismatch")
		}
		return v.AccessToken
	}
	send := func(realm, email, password string) string {
		t.Helper()
		for _, key := range []string{proofs.Key(realm, email), proofs.Key(realm, email) + ":cooldown"} {
			if err := testRun.Record("redis", key); err != nil {
				t.Fatal(err)
			}
		}
		_, raw := call("POST", "/v1/"+realm+"/auth/code", "", map[string]string{"email": email, "password": password, "locale": "en"}, 200, "OK")
		var v types.ChallengeData
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v.Challenge
	}
	call("POST", "/v1/admin/auth/code", "", map[string]string{"email": admin.Email, "password": "wrong", "locale": "en"}, 400, "AUTH_PROOF_INVALID")
	proof := send("admin", admin.Email, initial)
	input := map[string]string{"email": admin.Email, "password": initial, "challenge": proof, "code": mail.code(admin.Email)}
	input["password"] = "wrong"
	call("POST", "/v1/admin/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	input["password"] = initial
	input["code"] = "000000"
	if input["code"] == mail.code(admin.Email) {
		input["code"] = "111111"
	}
	call("POST", "/v1/admin/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	input["code"] = mail.code(admin.Email)
	h, b := call("POST", "/v1/admin/auth/login", "", input, 200, "OK")
	temporary := decodeToken(h, b)
	call("POST", "/v1/admin/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	call("GET", "/v1/admin/current", temporary, nil, 200, "OK")
	call("GET", "/v1/admin/accounts", temporary, nil, 403, "PERMISSION_DENIED")
	call("GET", "/v1/merchant/current", temporary, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/user/current", temporary, nil, 401, "UNAUTHENTICATED")
	h, b = call("POST", "/v1/admin/password", temporary, map[string]string{"oldPassword": initial, "password": changed}, 200, "OK")
	token := decodeToken(h, b)
	call("GET", "/v1/admin/current", temporary, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/admin/accounts", token, nil, 200, "OK")
	call("POST", "/v1/admin/admins", token, map[string]string{}, 404, "NOT_FOUND")
	call("POST", "/v1/admin/logout-all", token, nil, 404, "NOT_FOUND")
	adminIdx, _ := s.Backoffice.Idx(backoffice.Admin, admin.Id)
	call("POST", "/v1/admin/accounts/status", token, map[string]string{"realm": "admin", "idx": adminIdx, "status": "disabled", "reason": "last administrator"}, 409, "CONFLICT")
	_, b = call("POST", "/v1/admin/merchants", token, map[string]string{"email": "merchant@example.com", "password": initial, "reason": "onboard"}, 200, "OK")
	var merchant types.BackofficeAccount
	if err = json.Unmarshal(b, &merchant); err != nil {
		t.Fatal(err)
	}
	proof = send("merchant", merchant.Email, initial)
	input = map[string]string{"email": merchant.Email, "password": initial, "challenge": proof, "code": mail.code(merchant.Email)}
	h, b = call("POST", "/v1/merchant/auth/login", "", input, 200, "OK")
	mt := decodeToken(h, b)
	h, b = call("POST", "/v1/merchant/password", mt, map[string]string{"oldPassword": initial, "password": changed}, 200, "OK")
	mt = decodeToken(h, b)
	call("GET", "/v1/admin/accounts", mt, nil, 401, "UNAUTHENTICATED")
	call("GET", "/v1/merchant/current", mt, nil, 200, "OK")
	call("POST", "/v1/admin/accounts/status", token, map[string]string{"realm": "merchant", "idx": merchant.Idx, "status": "disabled", "reason": "suspend"}, 200, "OK")
	call("GET", "/v1/merchant/current", mt, nil, 401, "UNAUTHENTICATED")
	call("POST", "/v1/admin/accounts/status", token, map[string]string{"realm": "merchant", "idx": merchant.Idx, "status": "active", "reason": "restore"}, 200, "OK")
	call("GET", "/v1/merchant/current", mt, nil, 401, "UNAUTHENTICATED")

	// Managing a product user's status affects its own token version only.
	userKey := s.RedisKey("user", "1")
	if err = testRun.Record("redis", userKey); err != nil {
		t.Fatal(err)
	}
	if err = s.Redis.Del(ctx, userKey).Err(); err != nil {
		t.Fatal(err)
	}
	product, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, normalizedEmail(t, "product@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	productIdx, _ := s.UserIdx.Encode(product.User.Id)
	productToken, _, err := s.Tokens.Issue(product.User.Id, product.User.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", "/v1/user/current", productToken, nil, 200, "OK")
	call("GET", "/v1/admin/user?idx="+productIdx, token, nil, 200, "OK")
	for _, status := range []string{"disabled", "active"} {
		call("POST", "/v1/admin/user/status", token, map[string]string{"idx": productIdx, "status": status, "reason": "product status check"}, 200, "OK")
		if status == "disabled" {
			call("GET", "/v1/user/current", productToken, nil, 403, "PERMISSION_DENIED")
		} else {
			call("GET", "/v1/user/current", productToken, nil, 401, "UNAUTHENTICATED")
		}
	}
	// Reset revokes even a correct, unconsumed challenge from the earlier version.
	if err = s.Redis.Del(ctx, proofs.Key("merchant", merchant.Email)+":cooldown").Err(); err != nil {
		t.Fatal(err)
	}
	proof = send("merchant", merchant.Email, changed)
	call("POST", "/v1/admin/accounts/reset", token, map[string]string{"realm": "merchant", "idx": merchant.Idx, "password": initial, "reason": "reset"}, 200, "OK")
	input = map[string]string{"email": merchant.Email, "password": initial, "challenge": proof, "code": mail.code(merchant.Email)}
	call("POST", "/v1/merchant/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	if err = s.Redis.Del(ctx, proofs.Key("merchant", merchant.Email)+":cooldown").Err(); err != nil {
		t.Fatal(err)
	}
	input["challenge"] = send("merchant", merchant.Email, initial)
	input["code"] = mail.code(merchant.Email)
	if err = s.Redis.Expire(ctx, proofs.Key("merchant", merchant.Email), 0).Err(); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/merchant/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	if err = s.Redis.Del(ctx, proofs.Key("merchant", merchant.Email)+":cooldown").Err(); err != nil {
		t.Fatal(err)
	}
	input["challenge"] = send("merchant", merchant.Email, initial)
	input["code"] = mail.code(merchant.Email)
	input["code"] = "000000"
	if input["code"] == mail.code(merchant.Email) {
		input["code"] = "111111"
	}
	for i := 0; i < 5; i++ {
		call("POST", "/v1/merchant/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	}
	input["code"] = mail.code(merchant.Email)
	call("POST", "/v1/merchant/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	_, b = call("GET", "/v1/admin/audits", token, nil, 200, "OK")
	if strings.Contains(string(b), initial) || strings.Contains(string(b), changed) || strings.Contains(string(b), proof) || strings.Contains(string(b), mail.code(merchant.Email)) {
		t.Fatal("audit exposed credentials")
	}
	var audits types.BackofficeAuditData
	if err = json.Unmarshal(b, &audits); err != nil {
		t.Fatal(err)
	}
	if len(audits.Items) < 10 {
		t.Fatal("missing mutation/login audits", len(audits.Items))
	}
	// Concurrent server operations cannot disable both remaining administrators.
	second, err := s.Backoffice.Create(ctx, super, backoffice.Admin, "second@example.com", initial, "second")
	if err != nil {
		t.Fatal(err)
	}
	secondIdx, _ := s.Backoffice.Idx(backoffice.Admin, second.Id)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, idx := range []string{adminIdx, secondIdx} {
		wg.Add(1)
		go func(idx string) {
			defer wg.Done()
			_, e := s.Backoffice.SetStatus(ctx, super, backoffice.Admin, idx, "disabled", "concurrent check")
			results <- e
		}(idx)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("expected exactly one disable", success)
	}
	var active int
	if err = db.QueryRow("SELECT COUNT(*) FROM admin_user WHERE status='active'").Scan(&active); err != nil || active != 1 {
		t.Fatal("last admin invariant", active, err)
	}
	// Login audit is best-effort, but successful management audit remains transactional.
	if err = s.Redis.Del(ctx, proofs.Key("merchant", merchant.Email)+":cooldown").Err(); err != nil {
		t.Fatal(err)
	}
	proof = send("merchant", merchant.Email, initial)
	input = map[string]string{"email": merchant.Email, "password": initial, "challenge": proof, "code": mail.code(merchant.Email)}
	// Audit insertion failure rolls back the account mutation, and is not reported as success.
	if _, err = db.Exec("RENAME TABLE backoffice_audit TO unavailable_audit"); err != nil {
		t.Fatal(err)
	}
	call("POST", "/v1/merchant/auth/login", "", input, 200, "OK")
	input["password"] = "wrong"
	call("POST", "/v1/merchant/auth/login", "", input, 400, "AUTH_PROOF_INVALID")
	_, mutationErr := s.Backoffice.Create(ctx, super, backoffice.Admin, "rollback@example.com", initial, "rollback check")
	if _, err = db.Exec("RENAME TABLE unavailable_audit TO backoffice_audit"); err != nil {
		t.Fatal(err)
	}
	if mutationErr == nil {
		t.Fatal("audit failure silently succeeded")
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM admin_user WHERE email='rollback@example.com'").Scan(&count); err != nil || count != 0 {
		t.Fatal("mutation escaped transaction", count, err)
	}
	// Exercise the deployed CLI entry, including stdin-only password input.
	binary := filepath.Join(t.TempDir(), "super")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../../tools/super")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build super: %v\n%s", e, out)
	}
	configFile := filepath.Join(t.TempDir(), "platform.yaml")
	raw, e := os.ReadFile("../../etc/platform.yaml")
	if e != nil {
		t.Fatal(e)
	}
	raw = []byte(strings.Replace(string(raw), "Environment: local", "Environment: "+c.Environment, 1) + "\nAuth: {}\nBackoffice: {}\n")
	if e = os.WriteFile(configFile, raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("DEXWALLET_AUTH_SECRET", cfg.Secret)
	t.Setenv("DEXWALLET_ADMIN_TOKEN_SECRET", c.Backoffice.AdminSecret)
	t.Setenv("DEXWALLET_MERCHANT_TOKEN_SECRET", c.Backoffice.MerchantSecret)
	command := func(password string, args ...string) error {
		cmd := exec.CommandContext(ctx, binary, append([]string{"-f", configFile, "--reason", "CLI verification"}, args...)...)
		cmd.Stdin = strings.NewReader(password + "\n")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Logf("super error: %s", out)
		}
		return e
	}
	if e = command(initial, "--action", "create-admin", "--email", "cli@example.com", "--password-stdin"); e != nil {
		t.Fatal(e)
	}
	var cliID int64
	if e = db.QueryRow("SELECT id FROM admin_user WHERE email='cli@example.com'").Scan(&cliID); e != nil {
		t.Fatal(e)
	}
	cliIdx, _ := s.Backoffice.Idx(backoffice.Admin, cliID)
	if e = testRun.Record("redis", fmt.Sprintf("%s:backoffice:account:admin:%d", s.Namespace, cliID)); e != nil {
		t.Fatal(e)
	}
	if e = command(changed, "--action", "reset-admin", "--idx", cliIdx, "--password-stdin"); e != nil {
		t.Fatal(e)
	}
	if e = command("", "--action", "enable-admin", "--idx", cliIdx); e != nil {
		t.Fatal(e)
	}
	var version, required int
	if e = db.QueryRow("SELECT auth_version,must_change_password FROM admin_user WHERE id=?", cliID).Scan(&version, &required); e != nil || version != 2 || required != 1 {
		t.Fatal("CLI reset did not revoke/require change", version, required, e)
	}

}

func normalizedEmail(t *testing.T, raw string) userlogic.EmailAddress {
	t.Helper()
	address, err := userlogic.NormalizeEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	return address
}
