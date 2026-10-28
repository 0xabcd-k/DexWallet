// testenv validates local test connections and owns a child run's resource journal.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		return errors.New("usage: testenv check admin|http | run COMMAND ARGS... | cleanup RUN_DIRECTORY")
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			return errors.New("check requires admin or http")
		}
		name := "DEXWALLET_TEST_MYSQL_DSN"
		if args[1] == "http" {
			name = "DEXWALLET_TEST_HTTP_MYSQL_DSN"
		}
		_, e := testenv.ValidateDSN(os.Getenv(name), args[1])
		return e
	case "cleanup":
		if len(args) != 2 {
			return errors.New("cleanup requires exact run directory")
		}
		return testenv.Recover(args[1])
	case "run":
		if len(args) < 2 {
			return errors.New("run requires command")
		}
		r, e := testenv.NewRun()
		if e != nil {
			return e
		}
		fmt.Println("Test resources:", r.Dir)
		cmd := exec.Command(args[1], args[2:]...)
		cmd.Env = append(os.Environ(), "DEXWALLET_TEST_RUN_DIR="+r.Dir)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		signals := make(chan os.Signal, 2)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)
		if e = cmd.Start(); e != nil {
			return errors.Join(e, r.Cleanup())
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case e = <-done:
		case <-signals:
			// Kill only our group before recovering resources; no resource mutation races.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
			e = errors.New("test run interrupted")
		}
		return errors.Join(e, testenv.Recover(r.Dir))
	default:
		return errors.New("unknown testenv action")
	}
}
