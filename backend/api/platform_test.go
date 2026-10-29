//go:build linux || darwin || freebsd

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestStartupSignalProcess(t *testing.T) {
	if os.Getenv("DEXWALLET_STARTUP_TEST") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("api", flag.ContinueOnError)
	os.Args = []string{"api", "-f", "etc/platform.yaml"}
	if err := run(); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup was not cancelled: %v", err)
	}
}

func TestSignalCancelsDependencyInitialization(t *testing.T) {
	// Accept TCP but never send a MySQL handshake. This is a fake dependency,
	// not a connection to a database or shared middleware.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupSignalProcess$")
	cmd.Env = append(os.Environ(), "DEXWALLET_STARTUP_TEST=1",
		"DEXWALLET_MYSQL_DSN=test@tcp("+listener.Addr().String()+")/test",
		"GORACE=atexit_sleep_ms=0")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	var processErr error
	go func() { processErr = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { cancel(); <-exited })
	select {
	case conn := <-accepted:
		if conn == nil {
			t.Fatal("fake dependency failed to accept")
		}
		defer conn.Close()
	case <-ctx.Done():
		t.Fatal("application did not connect to fake dependency")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
		if processErr != nil {
			t.Fatalf("startup helper: %v\n%s", processErr, output.String())
		}
	case <-time.After(time.Second):
		t.Fatal("startup ignored shutdown; still waiting for the 3s dependency deadline")
	}
}
