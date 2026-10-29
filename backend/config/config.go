package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
	rest.RestConf
	Environment       string
	DependencyTimeout time.Duration `json:",default=3s"`
	ProbeTimeout      time.Duration `json:",default=1s"`
	ProbeInterval     time.Duration `json:",default=5s"`
	ProbeFreshness    time.Duration `json:",default=15s"`
	ShutdownTimeout   time.Duration `json:",default=30s"`
	MySQL             MySQL
	Redis             Redis
	RocketMQ          RocketMQ
}
type MySQL struct {
	DSN     string `json:",optional"`
	MaxOpen int    `json:",default=20"`
	MaxIdle int    `json:",default=5"`
}
type Redis struct {
	Enabled  bool
	Required bool
	Address  string `json:",optional"`
	Password string `json:",optional"`
	DB       int    `json:",default=0"`
}
type RocketMQ struct {
	Enabled       bool
	Required      bool
	NameServers   []string `json:",optional"`
	BrokerAddress string   `json:",optional"`
	AccessKey     string   `json:",optional"`
	SecretKey     string   `json:",optional"`
}

func Load(path string) (Config, error) {
	var c Config
	// Inject secrets after parsing. Repeated loads must not reuse cached env-tag values.
	if err := conf.Load(path, &c); err != nil {
		return c, fmt.Errorf("load config %q: %w", path, err)
	}
	c.MySQL.DSN = os.Getenv("DEXWALLET_MYSQL_DSN")
	c.Redis.Password = os.Getenv("DEXWALLET_REDIS_PASSWORD")
	c.RocketMQ.AccessKey = os.Getenv("DEXWALLET_MQ_ACCESS_KEY")
	c.RocketMQ.SecretKey = os.Getenv("DEXWALLET_MQ_SECRET_KEY")
	return c, c.Check()
}
func (c Config) Check() error {
	if c.Name == "" || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(c.Environment) {
		return fmt.Errorf("Name and valid Environment are required")
	}
	if c.Host == "" || c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid HTTP address")
	}
	if c.Timeout <= 0 || c.Timeout > int64((1<<63-1)/time.Millisecond) || c.DependencyTimeout <= 0 || c.DependencyTimeout > c.RequestDuration() || c.ProbeTimeout <= 0 || c.ProbeInterval <= 0 || c.ProbeFreshness < c.ProbeInterval+c.ProbeTimeout || c.ShutdownTimeout < c.RequestDuration() {
		return fmt.Errorf("invalid timeout budgets")
	}
	if c.Telemetry.Disabled || !c.Middlewares.Trace || !c.Middlewares.Timeout || !c.Middlewares.Recover || !c.Middlewares.MaxBytes || !c.Middlewares.MaxConns {
		return fmt.Errorf("JSON API requires trace, timeout, recovery and resource limits")
	}
	if len(c.TraceIgnorePaths) != 0 || c.Middlewares.Gunzip {
		return fmt.Errorf("JSON API requires trace IDs on every path and does not enable compressed requests")
	}
	if c.Middlewares.Breaker || c.Middlewares.Shedding {
		return fmt.Errorf("native breaker and shedding responses do not implement the JSON error contract; keep them disabled")
	}
	if c.Shutdown.WaitTime < c.Shutdown.WrapUpTime+c.ShutdownTimeout+c.DependencyTimeout || c.Shutdown.WrapUpTime <= 0 {
		return fmt.Errorf("process shutdown budget must exceed request drain and dependency cleanup")
	}
	if c.MaxConns < 1 || c.MaxBytes < 1 || c.MySQL.MaxOpen < 1 || c.MySQL.MaxIdle < 0 || c.MySQL.MaxIdle > c.MySQL.MaxOpen {
		return fmt.Errorf("invalid resource limits")
	}
	dsn, err := mysql.ParseDSN(c.MySQL.DSN)
	if err != nil || dsn.DBName == "" || dsn.User == "" {
		return fmt.Errorf("DEXWALLET_MYSQL_DSN must specify a valid user and database")
	}
	if dsn.MultiStatements {
		return fmt.Errorf("application DSN must not enable multiStatements")
	}
	if c.Redis.Required && !c.Redis.Enabled || c.RocketMQ.Required && !c.RocketMQ.Enabled {
		return fmt.Errorf("required dependency cannot be disabled")
	}
	if c.Redis.DB < 0 {
		return fmt.Errorf("invalid Redis DB")
	}
	if c.Redis.Enabled {
		if _, _, err := net.SplitHostPort(c.Redis.Address); err != nil {
			return fmt.Errorf("invalid Redis address")
		}
	}
	if c.RocketMQ.Enabled {
		if len(c.RocketMQ.NameServers) == 0 {
			return fmt.Errorf("RocketMQ NameServers required")
		}
		for _, a := range append(append([]string{}, c.RocketMQ.NameServers...), c.RocketMQ.BrokerAddress) {
			if _, _, err := net.SplitHostPort(a); err != nil {
				return fmt.Errorf("invalid RocketMQ address")
			}
		}
		if (c.RocketMQ.AccessKey == "") != (c.RocketMQ.SecretKey == "") {
			return fmt.Errorf("both RocketMQ credentials are required together")
		}
	}
	return nil
}
func (c Config) Namespace() string { return "dexwallet_" + c.Environment }

// RestConf.Timeout uses milliseconds; application budgets use time.Duration.
func (c Config) RequestDuration() time.Duration { return time.Duration(c.Timeout) * time.Millisecond }
