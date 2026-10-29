// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/server"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
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
	// Install the framework budget and cancellation before opening dependencies:
	// a signal may arrive while the database handshake or first probe is pending.
	proc.SetTimeToForceQuit(c.Shutdown.WaitTime)
	ctx, cancelStartup := context.WithCancel(context.Background())
	defer cancelStartup()
	proc.AddWrapUpListener(cancelStartup)
	svcCtx, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, svcCtx.Close()) }()
	app, rt, err := server.Build(svcCtx)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	logx.Infow("starting api", logx.Field("host", c.Host), logx.Field("port", c.Port), logx.Field("environment", c.Environment))
	return server.Start(app, rt)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
