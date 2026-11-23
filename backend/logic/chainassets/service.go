package chainassets

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const queryRateKey = "chain_assets:query:user:"
const defaultTimeout = 3 * time.Second
const defaultPerMinute = 30
const queryBurst = 5

type Endpoint struct {
	Network          string
	URL              string
	APIKey           string `json:",optional"`
	TestUSDTContract string `json:",optional"`
}
type Config struct {
	DepositQuotes    *DepositQuoteConfig `json:",optional"`
	Networks         []Endpoint          `json:",optional"`
	Timeout          time.Duration       `json:",optional"`
	PerUserPerMinute int                 `json:",optional"`
}

func (c Config) Check() error {
	if c.Timeout < 0 || c.Timeout > 30*time.Second || c.PerUserPerMinute < 0 {
		return fmt.Errorf("invalid chain query budgets")
	}
	seen := map[string]bool{}
	available := map[string]Network{}
	for _, n := range catalog() {
		available[n.ID] = n
	}
	for _, e := range c.Networks {
		n, ok := available[e.Network]
		if !ok || seen[e.Network] {
			return fmt.Errorf("unknown or duplicate chain query network: %s", e.Network)
		}
		seen[e.Network] = true
		u, err := url.Parse(e.URL)
		if err != nil {
			return fmt.Errorf("invalid chain endpoint for %s", e.Network)
		}
		ip := net.ParseIP(u.Hostname())
		local := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
		if u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && local)) {
			return fmt.Errorf("chain endpoint requires HTTPS or loopback HTTP: %s", e.Network)
		}
		if e.TestUSDTContract != "" {
			if !n.Testnet || n.Family == Base {
				return fmt.Errorf("USDT substitutes only supported on TON/Solana/TRON test networks")
			}
			if _, err := normalizeAddress(n, e.TestUSDTContract); err != nil {
				return fmt.Errorf("invalid test USDT identity on %s: %w", n.ID, err)
			}
		}
	}
	return nil
}

type Balance struct {
	AssetID, Quantity, Status, ErrorCode string
	QueriedAt                            int64
}
type Result struct {
	Network, Address string
	Items            []Balance
}
type Limiter interface {
	Allow(context.Context, int64) error
}
type RedisLimiter struct {
	client    *redis_rate.Limiter
	namespace string
	perMinute int
}

func NewRedisLimiter(client *redis.Client, namespace string, rate int) *RedisLimiter {
	if rate == 0 {
		rate = defaultPerMinute
	}
	return &RedisLimiter{redis_rate.NewLimiter(client), namespace, rate}
}
func (l *RedisLimiter) Allow(ctx context.Context, user int64) error {
	r, err := l.client.Allow(ctx, l.namespace+":"+queryRateKey+strconv.FormatInt(user, 10), redis_rate.Limit{Rate: l.perMinute, Burst: queryBurst, Period: time.Minute})
	if err != nil {
		return apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query limiter: %w", err))
	}
	if r.Allowed == 0 {
		return apperror.New(apperror.RateLimited, nil)
	}
	return nil
}

type Service struct {
	marketRates MarketRateReader
	quotes      depositBook
	networks    []Network
	endpoints   map[string]Endpoint
	client      *http.Client
	timeout     time.Duration
	limiter     Limiter
}

func New(c Config, limiter Limiter) (*Service, error) { return NewWithMarketRates(c, limiter, nil) }

func NewWithMarketRates(c Config, limiter Limiter, rates MarketRateReader) (*Service, error) {
	if c.DepositQuotes != nil && c.DepositQuotes.Market != nil && rates == nil {
		return nil, fmt.Errorf("deposit market rates require a rate reader")
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 4
	transport.MaxConnsPerHost = 16
	s := &Service{marketRates: rates, networks: catalog(), endpoints: map[string]Endpoint{}, timeout: timeout, limiter: limiter,
		client: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	for _, e := range c.Networks {
		s.endpoints[e.Network] = e
	}
	for i := range s.networks {
		n := &s.networks[i]
		e, ok := s.endpoints[n.ID]
		n.Enabled = ok
		if e.TestUSDTContract != "" {
			n.Assets = append(n.Assets, asset(n.ID, "USDT", e.TestUSDTContract, 6, TestFixture))
		}
	}
	book, err := newDepositBook(c.DepositQuotes, s.networks, s.endpoints[SolanaDevnet].TestUSDTContract, s.endpoints[TONTestnet].TestUSDTContract)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.quotes = book
	return s, nil
}
func (s *Service) Close() { s.client.CloseIdleConnections() }
func (s *Service) Catalog() []Network {
	out := append([]Network(nil), s.networks...)
	for i := range out {
		out[i].Assets = append([]Asset(nil), out[i].Assets...)
	}
	return out
}
func (s *Service) Query(ctx context.Context, user int64, networkID, address string) (Result, error) {
	var network Network
	for _, n := range s.networks {
		if n.ID == networkID {
			network = n
			break
		}
	}
	if network.ID == "" {
		return Result{}, apperror.New(apperror.InvalidArgument, fmt.Errorf("unknown network"))
	}
	owner, err := normalizeAddress(network, address)
	if err != nil {
		return Result{}, apperror.New(apperror.InvalidArgument, err)
	}
	if !network.Enabled {
		return Result{}, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query network %s disabled", networkID))
	}
	if s.limiter == nil {
		return Result{}, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query limiter not configured"))
	}
	if err = s.limiter.Allow(ctx, user); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	result := Result{Network: network.ID, Address: owner, Items: make([]Balance, len(network.Assets))}
	endpoint := s.endpoints[networkID]
	// Verify networks where the public RPC exposes a stable identifier. TON/TRON
	// endpoints must be mapped by the operator to the documented network host.
	networkErr := s.checkNetwork(ctx, network, endpoint)
	var wait sync.WaitGroup
	for i, a := range network.Assets {
		wait.Add(1)
		go func() {
			defer wait.Done()
			var quantity string
			err := networkErr
			if err == nil {
				quantity, err = s.balance(ctx, network, endpoint, owner, a)
			}
			b := Balance{AssetID: a.ID, Quantity: quantity, Status: StatusOK, ErrorCode: ErrorNone, QueriedAt: time.Now().UnixMilli()}
			if err != nil {
				b.Quantity = ""
				b.Status = StatusUnavailable
				b.ErrorCode = ErrorUpstream
				var failure *upstreamError
				if errors.As(err, &failure) {
					b.ErrorCode = failure.code
				}
				logx.WithContext(ctx).Errorw("chain asset query failed", logx.Field("network", network.ID), logx.Field("asset", a.ID), logx.Field("address", owner), logx.Field("cause", err.Error()))
			}
			result.Items[i] = b
		}()
	}
	wait.Wait()
	return result, nil
}
func endpointURL(e Endpoint, path string, query url.Values) string {
	if path == "" {
		return e.URL
	}
	u, _ := url.Parse(e.URL)
	u.Path = strings.TrimRight(u.Path, "/") + path
	q := u.Query()
	for k, values := range query {
		q[k] = values
	}
	u.RawQuery = q.Encode()
	return u.String()
}
