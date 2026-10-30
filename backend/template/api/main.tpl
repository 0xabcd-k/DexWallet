// Code scaffolded by goctl. Safe to edit.
// goctl {{.version}}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"{{.projectPkg}}/__API_DIR__/internal/server"
	"{{.projectPkg}}/config"
	"{{.projectPkg}}/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/trace"
)

func run() (err error) {
	path := flag.String("f", "__API_DIR__/etc/{{.serviceName}}.yaml", "configuration file")
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
	if err = svcCtx.StartBackground(ctx); err != nil { return err }
	if err = ctx.Err(); err != nil { return err }
	var backgroundErr error
	if svcCtx.Background != nil {
		proc.AddWrapUpListener(svcCtx.Background.Stop)
		proc.AddShutdownListener(func() {
			svcCtx.Background.Stop()
			drain, cancel := context.WithTimeout(context.Background(), c.ShutdownTimeout)
			defer cancel()
			backgroundErr = svcCtx.Background.Wait(drain)
			if backgroundErr != nil { logx.Errorf("background drain: %v", backgroundErr) }
		})
	}
	logx.Infow("starting api", logx.Field("host", c.Host), logx.Field("port", c.Port), logx.Field("environment", c.Environment))
	err = server.Start(app, rt)
	return errors.Join(err, backgroundErr)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
