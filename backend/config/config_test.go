package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentInjectionAndValidation(t *testing.T) {
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
	for _, mutate := range []func(*Config){func(c *Config) { c.MySQL.DSN = "" }, func(c *Config) { c.Redis.Required = true; c.Redis.Enabled = false }, func(c *Config) { c.DependencyTimeout = 2 * c.RequestTimeout }, func(c *Config) { c.ProbeFreshness = c.ProbeInterval }, func(c *Config) { c.Environment = "other:project" }, func(c *Config) { c.RocketMQ.NameServers = nil }} {
		bad := c
		mutate(&bad)
		if bad.Check() == nil {
			t.Fatal("invalid config accepted")
		}
	}
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
