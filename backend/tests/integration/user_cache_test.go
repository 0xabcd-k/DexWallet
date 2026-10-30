//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestUserCache(t *testing.T) {
	if err := testenv.CheckLocalDependencies(testenv.RedisAddress, []string{testenv.NameServer}, testenv.BrokerAddress); err != nil {
		t.Fatal(err)
	}
	db, _ := userDatabase(t, "user_cache")
	client := redis.NewClient(&redis.Options{Addr: testenv.RedisAddress, Password: os.Getenv("DEXWALLET_REDIS_PASSWORD"), ContextTimeoutEnabled: true, MaxRetries: -1})
	defer client.Close()
	cache := model.NewUserCache(client, testRun.Name("redis", "user_cache"))
	for _, id := range []int64{1, 2} {
		if err := testRun.Record("redis", cache.Key(id)); err != nil {
			t.Fatal(err)
		}
	}
	conn := sqlx.NewSqlConnFromDB(db)
	m := model.NewUserModelWithCache(conn, cache)
	tx := transaction.New(db)
	users := userlogic.New(m, model.NewEmailIdentityModel(conn), model.NewTelegramIdentityModel(conn), tx)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mustExec(t, db, "INSERT INTO `user`(id) VALUES(1),(2)")
	first, err := users.FindUserByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	ttl, err := client.PTTL(ctx, cache.Key(1)).Result()
	if err != nil || ttl <= 29*time.Second || ttl > model.UserCacheTTL {
		t.Fatalf("initial TTL=%v err=%v", ttl, err)
	}

	// Simulate a committed write whose invalidation was lost. Even continuous
	// traffic must see the database update after the original 30-second window.
	mustExec(t, db, "UPDATE `user` SET status='disabled',auth_version=2 WHERE id=1")
	err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		locked, err := m.WithSession(session).FindForUpdate(ctx, 1)
		if err != nil || locked.Status != model.StatusDisabled {
			t.Fatalf("locking read used cache: %+v %v", locked, err)
		}
		ordinary, err := m.WithSession(session).FindOne(ctx, 1)
		if err != nil || ordinary.AuthVersion != 2 {
			t.Fatalf("transaction read used cache: %+v %v", ordinary, err)
		}
		_, err = users.CheckActorInSession(ctx, session, userlogic.Actor{UserID: 1, AuthVersion: first.AuthVersion})
		return err
	})
	if !errors.Is(err, userlogic.ErrDisabled) {
		t.Fatalf("critical authorization accepted stale state: %v", err)
	}
	start := time.Now()
	reads := 0
	for {
		got, err := users.FindUserByID(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		reads++
		if got.Status == model.StatusDisabled {
			if time.Since(start) < 28*time.Second || got.AuthVersion != 2 {
				t.Fatalf("unexpected expiry: %+v after %v", got, time.Since(start))
			}
			break
		}
		if time.Since(start) > model.UserCacheTTL+2*time.Second {
			t.Fatal("frequent reads extended cache TTL")
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("%d reads refreshed committed state after %v", reads, time.Since(start))

	rejected := errors.New("rollback requested")
	before, err := client.Get(ctx, cache.Key(1)).Result()
	if err != nil {
		t.Fatal(err)
	}
	err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		local := m.WithSession(session)
		u, err := local.FindForUpdate(ctx, 1)
		if err != nil {
			return err
		}
		u.Status, u.AuthVersion = model.StatusActive, 3
		if err = local.Update(ctx, u); err != nil {
			return err
		}
		inside, err := local.FindOne(ctx, 1)
		if err != nil || inside.Status != model.StatusActive {
			t.Fatalf("own write hidden: %+v %v", inside, err)
		}
		outside, err := m.FindOne(ctx, 1)
		if err != nil || outside.Status != model.StatusDisabled {
			t.Fatalf("uncommitted state leaked: %+v %v", outside, err)
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if after, err := client.Get(ctx, cache.Key(1)).Result(); err != nil || after != before {
		t.Fatalf("rollback changed cache: %v", err)
	}

	err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		local := m.WithSession(session)
		u, err := local.FindForUpdate(ctx, 1)
		if err != nil {
			return err
		}
		u.Status, u.AuthVersion = model.StatusActive, 3
		if err = local.Update(ctx, u); err != nil {
			return err
		}
		if value, err := client.Get(ctx, cache.Key(1)).Result(); err != nil || value != before {
			t.Fatalf("invalidated before outer commit: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := client.Exists(ctx, cache.Key(1)).Result(); err != nil || count != 0 {
		t.Fatalf("commit did not invalidate: %d %v", count, err)
	}
	if got, err := users.FindUserByID(ctx, 1); err != nil || got.AuthVersion != 3 {
		t.Fatalf("post commit read: %+v %v", got, err)
	}

	// Exercise invalidation through the actual outer auth operation transaction.
	actor := userlogic.Actor{UserID: 1, AuthVersion: 3}
	for _, kind := range []model.IdentityType{model.IdentityEmail, model.IdentityTelegram} {
		err = tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
			_, err := users.ChangeIdentityInSession(ctx, session, actor, userlogic.Bind, kind, "cache@example.com", &userlogic.VerifiedTelegram{TelegramUserID: 12345, FirstName: "Cache"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg := authlogic.DefaultConfig()
	cfg.Secret = authTestSecret
	auth, err := authlogic.New(cfg, users, authlogic.NewProofs(client, "unused-proof-store", cfg.Secret), nil, allowAuth{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := auth.Unbind(ctx, actor, model.IdentityEmail)
	if err != nil || result.User.AuthVersion != 4 {
		t.Fatalf("unbind: %+v %v", result, err)
	}
	if count, err := client.Exists(ctx, cache.Key(1)).Result(); err != nil || count != 0 {
		t.Fatalf("auth commit did not invalidate: %d %v", count, err)
	}
	if got, err := m.FindOne(ctx, 1); err != nil || got.AuthVersion != 4 {
		t.Fatalf("auth version cache: %+v %v", got, err)
	}
	if _, err := auth.Unbind(ctx, actor, model.IdentityEmail); !errors.Is(err, userlogic.ErrStaleSession) {
		t.Fatal("stale actor accepted", err)
	}

	if _, err := m.FindOne(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := m.FindOne(ctx, 2); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted user cached: %v", err)
	}
	client.Close()
	if got, err := m.FindOne(ctx, 1); err != nil || got.AuthVersion != 4 {
		t.Fatalf("Redis failure did not fall back to DB: %v", err)
	}
	// Failed invalidation cannot turn an already committed write into a failure.
	if err := m.Update(ctx, &model.User{Id: 1, Status: model.StatusActive, AuthVersion: 5}); err != nil {
		t.Fatalf("committed update reported failed: %v", err)
	}
	var version int64
	if err := db.QueryRowContext(ctx, "SELECT auth_version FROM `user` WHERE id=1").Scan(&version); err != nil || version != 5 {
		t.Fatalf("write outcome lost: %d %v", version, err)
	}
}
