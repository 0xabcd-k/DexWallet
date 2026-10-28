//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
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
	t.Setenv("DEXWALLET_MYSQL_DSN", cfg.FormatDSN())
	c, err := config.Load("../../api/etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Environment = "integration_" + testRun.ID
	if err = testenv.CheckLocalDependencies(c.Redis.Address, c.RocketMQ.NameServers, c.RocketMQ.BrokerAddress); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	monitor := health.New(d.Checks, c.ProbeFreshness)
	monitor.Refresh(ctx, d.Checks, c.ProbeTimeout, nil)
	for _, state := range monitor.Snapshot() {
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
	if !monitor.Ready() {
		t.Fatal("not ready")
	}
}
