// Code scaffolded by goctl. Safe to edit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/server"
	apisvc "github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

func run() (err error) {
	path := flag.String("f", "api/etc/platform.yaml", "configuration file")
	flag.Parse()
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	if err = logx.SetUp(c.Log); err != nil {
		return err
	}
	defer logx.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	deps, err := svc.Open(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, deps.Close()) }()
	monitor := health.New(deps.Checks, c.ProbeFreshness)
	report := func(s health.State) {
		fields := []logx.LogField{logx.Field("dependency", s.Name), logx.Field("required", s.Required)}
		if s.Err != nil {
			logx.Errorw("dependency unavailable", append(fields, logx.Field("cause", s.Err.Error()))...)
		} else {
			logx.Infow("dependency available", fields...)
		}
	}
	monitor.Refresh(ctx, deps.Checks, c.ProbeTimeout, report)
	checks, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		monitor.Run(checks, deps.Checks, c.ProbeInterval, c.ProbeTimeout, report)
	}()
	defer func() { cancel(); <-finished }()
	app, err := server.Build(&apisvc.ServiceContext{Config: c, Dependencies: deps, Health: monitor})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(c.Host, fmt.Sprint(c.Port)))
	if err != nil {
		return err
	}
	logx.Infow("api listening", logx.Field("address", ln.Addr().String()), logx.Field("environment", c.Environment))
	return app.Serve(ctx, ln, c.ShutdownTimeout)
}
func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
