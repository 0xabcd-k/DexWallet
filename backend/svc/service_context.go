package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	authmodel "github.com/0xabcd-k/DexWallet/backend/model/auth"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/admin"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/rest"
)

const (
	MySQLDependency = "mysql"
	RedisDependency = "redis"
	MQDependency    = "rocketmq"
)

// ServiceContext owns shared clients and health checks. API handlers and future
// background services receive the same instance; only main closes it.
type ServiceContext struct {
	Config                config.Config
	UserIdx               *idcodec.Codec
	Users                 *userlogic.Service
	Auth                  *authlogic.Service
	Tokens                *session.Tokens
	AuthTransport         rest.Middleware
	UserAuth              rest.Middleware
	IdentityAuth          rest.Middleware
	ModelUser             usermodel.UserModel
	ModelEmailIdentity    usermodel.EmailIdentityModel
	ModelTelegramIdentity usermodel.TelegramIdentityModel
	Health                *health.Monitor
	DB                    *sql.DB
	Tx                    *transaction.Runner
	Redis                 *redis.Client
	Producer              rocketmq.Producer
	mqAdmin               admin.Admin
	Namespace             string
	checks                []health.Check
	stopProbe             context.CancelFunc
	probeDone             chan struct{}
	once                  sync.Once
	closeErr              error
}

// NewServiceContext initializes clients and health checks. Failed startup closes
// already-created resources; successful startup transfers ownership to the caller.
func NewServiceContext(ctx context.Context, c config.Config) (d *ServiceContext, err error) {
	if err = c.Check(); err != nil {
		return nil, err
	}
	d = &ServiceContext{Config: c, Namespace: c.Namespace()}
	d.UserIdx, err = idcodec.New(c.IDSecret, idcodec.User)
	if err != nil {
		return nil, err
	}
	if c.Session != nil {
		d.Tokens, err = session.New(*c.Session, c.Namespace(), d.UserIdx)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, d.Close())
			d = nil
		}
	}()
	cfg, _ := mysql.ParseDSN(c.MySQL.DSN)
	cfg.Timeout = c.DependencyTimeout
	cfg.ReadTimeout = c.DependencyTimeout
	cfg.WriteTimeout = c.DependencyTimeout
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	d.DB, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return d, fmt.Errorf("mysql open: %w", err)
	}
	d.DB.SetMaxOpenConns(c.MySQL.MaxOpen)
	d.DB.SetMaxIdleConns(c.MySQL.MaxIdle)
	d.DB.SetConnMaxLifetime(5 * time.Minute)
	call, cancel := context.WithTimeout(ctx, c.DependencyTimeout)
	defer cancel()
	if err = d.DB.PingContext(call); err != nil {
		return d, fmt.Errorf("mysql startup: %w", err)
	}
	d.Tx = transaction.New(d.DB)
	conn := sqlx.NewSqlConnFromDB(d.DB)
	d.ModelUser = usermodel.NewUserModel(conn)
	d.ModelEmailIdentity = usermodel.NewEmailIdentityModel(conn)
	d.ModelTelegramIdentity = usermodel.NewTelegramIdentityModel(conn)
	d.checks = append(d.checks, health.Check{Name: MySQLDependency, Required: true, Run: d.DB.PingContext})
	if c.Redis.Enabled {
		d.Redis = redis.NewClient(&redis.Options{Addr: c.Redis.Address, Password: c.Redis.Password, DB: c.Redis.DB, DialTimeout: c.DependencyTimeout, ReadTimeout: c.DependencyTimeout, WriteTimeout: c.DependencyTimeout, ContextTimeoutEnabled: true, MaxRetries: -1})
		d.ModelUser = usermodel.NewUserModelWithCache(conn, usermodel.NewUserCache(d.Redis, d.Namespace))
		d.checks = append(d.checks, health.Check{Name: RedisDependency, Required: c.Redis.Required, Run: func(ctx context.Context) error { return d.Redis.Ping(ctx).Err() }})
	}
	d.Users = userlogic.New(d.ModelUser, d.ModelEmailIdentity, d.ModelTelegramIdentity, d.Tx)
	if c.Auth != nil {
		var sender authlogic.Mailer
		if c.Auth.EmailEnabled {
			sender, err = authlogic.NewSMTPMailer(c.Auth.SMTP)
			if err != nil {
				return d, err
			}
		}
		d.Auth, err = authlogic.New(*c.Auth, d.Users, authmodel.NewEmailChallengeModel(conn), authmodel.NewAuthOperationModel(conn), d.Tx, sender, authlogic.NewRedisLimiter(d.Redis, d.Namespace, *c.Auth))
		if err != nil {
			return d, err
		}
	}
	if c.RocketMQ.Enabled {
		setupMQLogging()
		credentials := primitive.Credentials{AccessKey: c.RocketMQ.AccessKey, SecretKey: c.RocketMQ.SecretKey}
		d.Producer, err = rocketmq.NewProducer(producer.WithNameServer(primitive.NamesrvAddr(c.RocketMQ.NameServers)), producer.WithGroupName(d.Namespace+"_api"), producer.WithInstanceName(d.Namespace+"_api"), producer.WithCredentials(credentials), producer.WithRetry(0), producer.WithSendMsgTimeout(c.DependencyTimeout))
		if err != nil {
			return d, fmt.Errorf("rocketmq configure: %w", err)
		}
		if err = d.Producer.Start(); err != nil {
			return d, fmt.Errorf("rocketmq start: %w", err)
		}
		d.mqAdmin, err = admin.NewAdmin(admin.WithResolver(primitive.NewPassthroughResolver(c.RocketMQ.NameServers)), admin.WithCredentials(credentials))
		if err != nil {
			return d, fmt.Errorf("rocketmq diagnostics: %w", err)
		}
		d.checks = append(d.checks, health.Check{Name: MQDependency, Required: c.RocketMQ.Required, Run: func(ctx context.Context) error {
			// Read-only nameserver protocol check plus broker transport check, NOT a delivery guarantee.
			if _, err := d.mqAdmin.FetchAllTopicList(ctx); err != nil {
				return fmt.Errorf("nameserver: %w", err)
			}
			conn, err := (&net.Dialer{Timeout: c.ProbeTimeout}).DialContext(ctx, "tcp", c.RocketMQ.BrokerAddress)
			if err != nil {
				return fmt.Errorf("broker transport: %w", err)
			}
			return conn.Close()
		}})
	}
	if err = d.startHealth(ctx); err != nil {
		return d, err
	}
	return d, nil
}
func (d *ServiceContext) RedisKey(parts ...string) string {
	return d.Namespace + ":" + strings.Join(parts, ":")
}

// Close runs after HTTP/background work has drained. Stop probes before closing
// clients, including when startup failed partway through initialization.
func (d *ServiceContext) Close() error {
	d.once.Do(func() {
		if d.Health != nil {
			d.Health.Stop()
		}
		if d.stopProbe != nil {
			d.stopProbe()
			<-d.probeDone
		}
		if d.Producer != nil {
			d.closeErr = errors.Join(d.closeErr, d.Producer.Shutdown())
		}
		if d.mqAdmin != nil {
			d.closeErr = errors.Join(d.closeErr, d.mqAdmin.Close())
		}
		if d.Redis != nil {
			d.closeErr = errors.Join(d.closeErr, d.Redis.Close())
		}
		if d.DB != nil {
			d.closeErr = errors.Join(d.closeErr, d.DB.Close())
		}
	})
	return d.closeErr
}
