package server

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"github.com/zeromicro/go-zero/rest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAppCORSAndProtectedRoutes(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := config.Load("../../etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.CORSOrigins = []string{"https://localhost"}
	checks := []health.Check{{Name: "db", Required: true, Run: func(context.Context) error { return nil }}}
	h := health.New(checks, time.Minute)
	h.Refresh(context.Background(), checks, time.Second, nil)
	app, rt, err := Build(&svc.ServiceContext{Config: c, Health: h, Auth: &authlogic.Service{}, Tokens: &session.Tokens{}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := rest.NewServerless(app)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://localhost", "https://untrusted.example"} {
		req := httptest.NewRequest("OPTIONS", "/v1/user/current", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
		rec := httptest.NewRecorder()
		rt.Middleware(http.HandlerFunc(adapter.Serve)).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if origin == "https://localhost" {
			if rec.Header().Get("Access-Control-Allow-Origin") != origin || !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "X-Access-Token") {
				t.Fatal(rec.Header())
			}
		} else if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted origin allowed")
		}
	}
	req := httptest.NewRequest("GET", "/v1/user/current", nil)
	req.Header.Set("Origin", "https://localhost")
	rec := httptest.NewRecorder()
	rt.Middleware(http.HandlerFunc(adapter.Serve)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("CORS bypassed authentication: %d %s", rec.Code, rec.Body.String())
	}
}
