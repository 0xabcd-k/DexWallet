//go:build integration

// appdemo serves the real API against a disposable local test database.
// Verification codes go to a private file, never to an HTTP endpoint.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/server"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	account "github.com/0xabcd-k/DexWallet/backend/logic/account"
	auth "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	tasks "github.com/0xabcd-k/DexWallet/backend/logic/task"
	users "github.com/0xabcd-k/DexWallet/backend/logic/user"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/rest"
)

const appEmail = "app@example.test"
const sampleTelegramID int64 = 900000001
const sampleChannelID int64 = -100900000001

type fileMail struct{ dir string }

func (m fileMail) SendCode(_ context.Context, address, code, _ string) error {
	data, err := json.Marshal(map[string]string{"address": address, "code": code})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dir, "mail.json"), data, 0600)
}
func run() (result error) {
	run, err := testenv.NewRun()
	if err != nil {
		return err
	}
	lock, err := testenv.Lock(run.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	defer func() { result = errors.Join(result, run.Cleanup()) }()
	fmt.Println("Private test evidence:", run.Dir)
	ctx := context.Background()
	name, dsn, db, err := run.NewDatabase(ctx, "app")
	if err != nil {
		return err
	}
	defer db.Close()
	migrations, err := migration.Open(dsn, name, "manifest/sql/diff/business", "business")
	if err != nil {
		return err
	}
	err = migrations.Up()
	a, b := migrations.Close()
	if err = errors.Join(err, a, b); err != nil {
		return err
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	parsed.MultiStatements = false
	if err = os.Setenv("DEXWALLET_MYSQL_DSN", parsed.FormatDSN()); err != nil {
		return err
	}
	if err = os.Setenv("DEXWALLET_ID_SECRET", "test-only-key123"); err != nil {
		return err
	}
	c, err := config.Load("api/etc/platform.yaml")
	if err != nil {
		return err
	}
	c.Environment = "integration_" + run.ID
	c.RocketMQ.Enabled = false
	c.Redis.Address = testenv.RedisAddress
	c.CORSOrigins = []string{"https://localhost"}
	cfg := auth.DefaultConfig()
	cfg.Secret = "test-only-auth-operation-secret-32"
	c.Auth = &cfg
	c.Session = &session.Config{Secret: "test-only-session-signing-secret-32"}
	s, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		return err
	}
	defer func() {
		for _, pattern := range []string{s.Namespace + ":*", "rate:" + s.Namespace + ":*"} {
			iter := s.Redis.Scan(ctx, 0, pattern, 100).Iterator()
			for iter.Next(ctx) {
				result = errors.Join(result, run.Record("redis", iter.Val()))
			}
			result = errors.Join(result, iter.Err())
		}
		result = errors.Join(result, s.Close())
	}()
	cfg.EmailEnabled = true
	s.Auth, err = auth.New(cfg, s.Users, auth.NewProofs(s.Redis, s.Namespace, cfg.Secret), fileMail{run.Dir}, auth.NewRedisLimiter(s.Redis, s.Namespace, cfg))
	if err != nil {
		return err
	}
	var payer *users.Resolved
	var recipientIdx string
	for _, address := range []string{appEmail, "recipient@example.test"} {
		email, err := users.NormalizeEmail(address)
		if err != nil {
			return err
		}
		resolved, err := s.Users.ResolveOrCreateVerifiedEmail(ctx, email)
		if err != nil {
			return err
		}
		if address == appEmail {
			payer = resolved
		} else {
			recipientIdx, err = s.UserIdx.Encode(resolved.User.Id)
			if err != nil {
				return err
			}
		}
	}
	err = s.Tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		_, err := s.Users.ChangeIdentityInSession(ctx, tx, users.Actor{UserID: payer.User.Id, AuthVersion: payer.User.AuthVersion}, users.Bind, usermodel.IdentityTelegram, users.EmailAddress{}, &users.VerifiedTelegram{TelegramUserID: sampleTelegramID, FirstName: "App sample"})
		if err != nil {
			return err
		}
		_, err = s.Accounts.Post(ctx, tx, account.Posting{Source: account.Source{Type: account.BusinessDeposit, ID: 1, Action: account.ActionPost}, Entries: []account.Entry{account.Credit(payer.User.Id, accounts.KindUSD, decimal.NewFromInt(100)), {System: account.SystemUSDClearing, Side: account.SideDebit, Amount: decimal.NewFromInt(100)}}})
		return err
	})
	if err != nil {
		return err
	}
	membership, err := tasks.NewMock([]tasks.Fixture{{ChannelID: sampleChannelID, TelegramUserID: sampleTelegramID, Status: tasks.MemberRegular}})
	if err != nil {
		return err
	}
	s.Tasks = tasks.NewWithRewards(sqlx.NewSqlConnFromDB(s.DB), membership, s.Tx, s.Accounts)
	_, err = s.DB.ExecContext(ctx, "INSERT INTO task_definition(kind,status,verification_mode,title,description,base_points,channel_id,starts_at,ends_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6),DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY))", tasks.KindTelegramChannelFollow, tasks.StatusActive, tasks.ModeMock, "App mock task", "No real Telegram subscription", 100, sampleChannelID)
	if err != nil {
		return err
	}
	app, rt, err := server.Build(s)
	if err != nil {
		return err
	}
	adapter, err := rest.NewServerless(app)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:18889")
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: rt.Middleware(http.HandlerFunc(adapter.Serve)), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	userIdx, err := s.UserIdx.Encode(payer.User.Id)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]string{"email": appEmail, "userIdx": userIdx, "recipientIdx": recipientIdx, "apiURL": "http://127.0.0.1:18889"})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(run.Dir, "app.json"), metadata, 0600); err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-signals:
	case err = <-done:
	}
	rt.Stop()
	drain, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return errors.Join(err, httpServer.Shutdown(drain), rt.Wait(drain))
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
