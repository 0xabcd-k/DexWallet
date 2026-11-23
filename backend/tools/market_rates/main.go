// market_rates publishes public exchange trades into the shared USD rate table.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/svc"
)

func run() (err error) {
	path := flag.String("f", "api/etc/platform.yaml", "application configuration")
	interval := flag.Duration("interval", 0, "refresh interval; zero runs once")
	flag.Parse()
	if *interval < 0 {
		return fmt.Errorf("negative polling interval")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if cfg.ChainAssets.DepositQuotes == nil || cfg.ChainAssets.DepositQuotes.Market == nil {
		return fmt.Errorf("configure ChainAssets.DepositQuotes.Market first")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := svc.NewServiceContext(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, app.Close()) }()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for {
		refreshErr := app.MarketRates.Refresh(ctx, client)
		if ctx.Err() != nil {
			return nil
		}
		if *interval == 0 {
			table, err := app.MarketRates.Current(ctx)
			if err != nil {
				return errors.Join(refreshErr, err)
			}
			return errors.Join(refreshErr, json.NewEncoder(os.Stdout).Encode(table))
		}
		if refreshErr != nil {
			fmt.Fprintln(os.Stderr, refreshErr)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*interval):
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
