//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	driver "github.com/go-sql-driver/mysql"
)

func TestDependencyClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, dsn, db, err := testRun.NewDatabase(ctx, "dependencies")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg, _ := driver.ParseDSN(dsn)
	cfg.MultiStatements = false
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", cfg.FormatDSN())
	c, err := config.Load("../../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Environment = "integration_" + testRun.ID
	authConfig := authlogic.DefaultConfig()
	authConfig.Secret = authTestSecret
	authConfig.TelegramEnabled = true
	authConfig.BotToken = authTestBot
	c.Auth = &authConfig
	if err = testenv.CheckLocalDependencies(c.Redis.Address, c.RocketMQ.NameServers, c.RocketMQ.BrokerAddress); err != nil {
		t.Fatal(err)
	}
	d, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if d.Auth == nil {
		t.Fatal("configured authentication service missing")
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	ddl, err := os.ReadFile("../../manifest/sql/ddl/user/tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, string(ddl))
	userKey := d.RedisKey("user", "1")
	if err = testRun.Record("redis", userKey); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO `user`(id) VALUES(1)")
	if _, err = d.Users.FindUserByID(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if ttl, err := d.Redis.PTTL(ctx, userKey).Result(); err != nil || ttl <= 0 || ttl > 30*time.Second {
		t.Fatalf("shared user cache not initialized: TTL=%v err=%v", ttl, err)
	}
	for _, state := range d.Health.Snapshot() {
		if state.Err != nil {
			t.Fatalf("%s: %v", state.Name, state.Err)
		}
		t.Log(state.Name, "probe passed")
	}
	key := d.RedisKey("probe")
	if err = testRun.Record("redis", key); err != nil {
		t.Fatal(err)
	}
	if err = d.Redis.Set(ctx, key, "isolated", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if value, err := d.Redis.Get(ctx, key).Result(); err != nil || value != "isolated" {
		t.Fatalf("redis roundtrip: %s %v", value, err)
	}
	// No MQ publish/consume here: route/broker diagnostics are not delivery proof.
	if !d.Health.Ready() {
		t.Fatal("not ready")
	}
}
