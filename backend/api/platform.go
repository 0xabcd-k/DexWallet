// Code scaffolded by goctl. Safe to edit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/server"
	apisvc "github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/trace"
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
	defer trace.StopAgent()
	ctx := context.Background()
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
	app, rt, err := server.Build(&apisvc.ServiceContext{Config: c, Dependencies: deps, Health: monitor})
	if err != nil {
		return err
	}
	logx.Infow("starting api", logx.Field("host", c.Host), logx.Field("port", c.Port), logx.Field("environment", c.Environment))
	return server.Start(app, rt)
}
func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
