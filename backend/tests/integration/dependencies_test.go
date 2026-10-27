//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	driver "github.com/go-sql-driver/mysql"
)

func TestDependencyClients(t *testing.T) {
	secret := os.Getenv("DEXWALLET_TEST_MYSQL_DSN")
	if secret == "" {
		t.Fatal("DEXWALLET_TEST_MYSQL_DSN required")
	}
	cfg, err := driver.ParseDSN(secret)
	if err != nil {
		t.Fatal("invalid DSN")
	}
	cfg.DBName = ""
	cfg.MultiStatements = false
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var id [8]byte
	rand.Read(id[:])
	name := "dexwallet_contract_" + hex.EncodeToString(id[:])
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
	}()
	cfg.DBName = name
	t.Setenv("DEXWALLET_MYSQL_DSN", cfg.FormatDSN())
	c, err := config.Load("../../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Environment = "integration_" + hex.EncodeToString(id[:])
	d, err := svc.Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	monitor := health.New(d.Checks, c.ProbeFreshness)
	monitor.Refresh(ctx, d.Checks, c.ProbeTimeout, nil)
	for _, state := range monitor.Snapshot() {
		if state.Err != nil {
			t.Fatalf("%s: %v", state.Name, state.Err)
		}
		t.Log(state.Name, "probe passed")
	}
	key := d.RedisKey("probe")
	if err = d.Redis.Set(ctx, key, "isolated", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	defer d.Redis.Del(context.Background(), key)
	if value, err := d.Redis.Get(ctx, key).Result(); err != nil || value != "isolated" {
		t.Fatalf("redis roundtrip: %s %v", value, err)
	}
	// No MQ publish/consume here: route/broker diagnostics are not delivery proof.
	if !monitor.Ready() {
		t.Fatal("not ready")
	}
}
