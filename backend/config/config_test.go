package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentInjectionAndValidation(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", `test:pass:#'word@tcp(127.0.0.1:3306)/dexwallet_test`)
	c, err := Load("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.MySQL.DSN, "pass:#'word") {
		t.Fatal("env credential changed")
	}
	if c.Namespace() != "dexwallet_local" {
		t.Fatal(c.Namespace())
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.IDSecret = "" }, func(c *Config) { c.IDSecret = "short" }, func(c *Config) { c.MySQL.DSN = "" }, func(c *Config) { c.Redis.Required = true; c.Redis.Enabled = false }, func(c *Config) { c.DependencyTimeout = 2 * c.RequestDuration() }, func(c *Config) { c.ProbeFreshness = c.ProbeInterval }, func(c *Config) { c.Environment = "other:project" }, func(c *Config) { c.RocketMQ.NameServers = nil }} {
		bad := c
		mutate(&bad)
		if bad.Check() == nil {
			t.Fatal("invalid config accepted")
		}
	}
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "")
	if _, err := Load("../api/etc/platform.yaml"); err == nil {
		t.Fatal("missing DSN accepted")
	}
	p := filepath.Join(t.TempDir(), "invalid.yaml")
	os.WriteFile(p, []byte("invalid: ["), 0600)
	if _, err := Load(p); err == nil {
		t.Fatal("bad YAML accepted")
	}
}

func TestFrameworkConfigBudgets(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := Load("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.RequestDuration() != 5*time.Second || !c.Middlewares.Timeout || !c.Middlewares.Metrics || !c.Middlewares.Prometheus {
		t.Fatal("framework defaults or duration units changed")
	}
	for name, mutate := range map[string]func(*Config){
		"disabled trace":                               func(c *Config) { c.Telemetry.Disabled = true },
		"disabled timeout":                             func(c *Config) { c.Middlewares.Timeout = false },
		"unadapted breaker":                            func(c *Config) { c.Middlewares.Breaker = true },
		"unadapted shedding":                           func(c *Config) { c.Middlewares.Shedding = true },
		"shutdown too short":                           func(c *Config) { c.Shutdown.WaitTime = c.ShutdownTimeout },
		"wrap-up omitted from budget":                  func(c *Config) { c.Shutdown.WaitTime = c.ShutdownTimeout + c.DependencyTimeout },
		"zero wrap-up falls back to framework default": func(c *Config) { c.Shutdown.WrapUpTime = 0 },
		"duration overflow":                            func(c *Config) { c.Timeout = 1<<63 - 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if bad.Check() == nil {
				t.Fatal("invalid framework config accepted")
			}
		})
	}
}

func TestAuthConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := Load("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth != nil {
		t.Fatal("auth should be disabled until explicitly configured")
	}
	data, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "auth.yaml")
	data = append(data, []byte("\nAuth:\n  TelegramEnabled: true\n")...)
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(file); err == nil {
		t.Fatal("missing auth secrets accepted")
	}
	t.Setenv("DEXWALLET_AUTH_SECRET", "test-only-auth-secret-32-characters")
	t.Setenv("DEXWALLET_TELEGRAM_BOT_TOKEN", "123:fake-token")
	c, err = Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.SendPerEmail != 10 || c.Auth.VerifyPerSource != 60 {
		t.Fatal("rate defaults missing")
	}
	c.Auth.TelegramDevelopment = true
	if err = c.Check(); err != nil {
		t.Fatal("local Telegram development rejected", err)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Environment = "production" }, func(c *Config) { c.Mode = "pro" }} {
		bad := c
		change(&bad)
		if bad.Check() == nil {
			t.Fatal("Telegram development allowed outside local dev")
		}
	}
	c.Redis.Enabled = false
	if c.Check() == nil {
		t.Fatal("auth without Redis allowed")
	}
}

func TestSessionConfiguration(t *testing.T) {
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	t.Setenv("DEXWALLET_AUTH_SECRET", "test-only-auth-secret-32-characters")
	t.Setenv("DEXWALLET_TOKEN_SECRET", "")
	data, err := os.ReadFile("../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "session.yaml")
	data = append(data, []byte("\nAuth: {}\nSession: {}\n")...)
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(file); err == nil {
		t.Fatal("missing token signing secret accepted")
	}
	t.Setenv("DEXWALLET_TOKEN_SECRET", "test-only-auth-secret-32-characters")
	if _, err = Load(file); err == nil {
		t.Fatal("shared signing/proof secret accepted")
	}
	t.Setenv("DEXWALLET_TOKEN_SECRET", "test-only-token-secret-32-characters")
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if c.Session == nil || c.Session.Secret != "test-only-token-secret-32-characters" {
		t.Fatal("session config not injected")
	}
	c.Auth = nil
	if c.Check() == nil {
		t.Fatal("public session without auth config accepted")
	}
}
