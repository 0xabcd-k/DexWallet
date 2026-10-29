//go:build linux || darwin || freebsd

package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/zeromicro/go-zero/rest"
)

// A subprocess exercises the real framework signal registry and listener. Calling
// proc.Shutdown in the test runner would modify process-global framework state.
func TestNativeLifecycleProcess(t *testing.T) {
	mode := os.Getenv("DEXWALLET_LIFECYCLE_TEST")
	if mode == "" {
		return
	}
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := config.Load("../../etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Port, _ = strconv.Atoi(os.Getenv("DEXWALLET_LIFECYCLE_PORT"))
	c.Timeout = 100
	c.ShutdownTimeout = 500 * time.Millisecond
	c.Shutdown.WrapUpTime = 300 * time.Millisecond
	c.Shutdown.WaitTime = 3 * time.Second
	if mode == "inflight" {
		c.Timeout = 2000
	}
	if mode == "force" {
		c.Timeout = 10000
		c.Shutdown.WaitTime = time.Second
	}
	checks := []health.Check{{Name: "fake-db", Required: true, Run: func(context.Context) error { return nil }}}
	h := health.New(checks, time.Minute)
	h.Refresh(context.Background(), checks, time.Second, nil)
	app, rt, err := Build(&svc.ServiceContext{Config: c, Health: h})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && scanner.Text() == "release" {
			close(release)
		}
	}()
	app.AddRoute(rest.Route{Method: http.MethodGet, Path: "/work", Handler: func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		fmt.Println("WORK_FINISHED")
		httpapi.JSON(r.Context(), w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
	}})
	app.AddRoute(rest.Route{Method: http.MethodGet, Path: "/entered", Handler: func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusConflict)
		}
	}})
	// Match main's ordering: native Start must finish before dependencies close.
	err = Start(app, rt)
	fmt.Println("DEPENDENCIES_CLOSED")
	if mode == "blocked" {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected diagnosed drain deadline, got %v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if h.Ready() {
		t.Fatal("still ready after shutdown")
	}
}

func TestNativeSignalShutdown(t *testing.T) {
	for _, mode := range []string{"inflight", "timedout", "blocked", "force"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeLifecycleProcess$")
			cmd.Env = append(os.Environ(), "DEXWALLET_LIFECYCLE_TEST="+mode, "DEXWALLET_LIFECYCLE_PORT="+strconv.Itoa(port))
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan struct{})
			var processErr error
			go func() { processErr = cmd.Wait(); close(exited) }()
			t.Cleanup(func() { cancel(); <-exited })
			client := &http.Client{Timeout: 3 * time.Second}
			defer client.CloseIdleConnections()
			url := "http://127.0.0.1:" + strconv.Itoa(port)
			waitHTTPStatus(t, client, url+"/health/ready", http.StatusOK)
			completed := make(chan int, 1)
			if mode == "force" {
				conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				// An unfinished header keeps native Shutdown waiting until the
				// process deadline; no application listener replaces that deadline.
				fmt.Fprint(conn, "GET /health/live HTTP/1.1\r\nHost: localhost\r\n")
			} else {
				go func() {
					res, err := client.Get(url + "/work")
					if err != nil {
						completed <- 0
						return
					}
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
					completed <- res.StatusCode
				}()
				if mode == "inflight" {
					waitHTTPStatus(t, client, url+"/entered", http.StatusOK)
				} else if status := <-completed; status != http.StatusServiceUnavailable {
					t.Fatalf("expected native timeout, got %d", status)
				}
			}
			signal := syscall.SIGTERM
			if mode == "inflight" {
				signal = syscall.SIGINT
			}
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if mode != "force" {
				waitHTTPStatus(t, client, url+"/health/ready", http.StatusServiceUnavailable)
				res, err := client.Get(url + "/work")
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if res.StatusCode != http.StatusServiceUnavailable {
					t.Fatalf("new work accepted during shutdown: %d", res.StatusCode)
				}
			}
			if mode == "inflight" || mode == "timedout" {
				fmt.Fprintln(input, "release")
			}
			if mode == "inflight" {
				if status := <-completed; status != http.StatusOK {
					t.Fatalf("in-flight work did not complete: %d", status)
				}
			}
			<-exited
			err = processErr
			if ctx.Err() != nil {
				t.Fatalf("native shutdown hung: %s", output.String())
			}
			if mode == "force" {
				if err == nil || !strings.Contains(output.String(), "force kill") || strings.Contains(output.String(), "DEPENDENCIES_CLOSED") {
					t.Fatalf("process deadline not enforced: %v\n%s", err, output.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("helper: %v\n%s", err, output.String())
			}
			if mode == "blocked" {
				if !strings.Contains(output.String(), "request work did not finish before shutdown deadline") {
					t.Fatalf("missing shutdown diagnostic: %s", output.String())
				}
			} else {
				finished := strings.Index(output.String(), "WORK_FINISHED")
				closed := strings.Index(output.String(), "DEPENDENCIES_CLOSED")
				if finished < 0 || closed < finished {
					t.Fatalf("dependencies closed before actual work ended: %s", output.String())
				}
			}
		})
	}
}

func waitHTTPStatus(t *testing.T, client *http.Client, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not return %d", url, want)
}
