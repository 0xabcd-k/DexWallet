package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func setup(t *testing.T, extra ...rest.Route) (*Server, *health.Monitor) {
	t.Helper()
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := config.Load("../../etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.RequestTimeout = 100 * time.Millisecond
	c.MaxInFlight = 1
	c.MaxBodyBytes = 100
	checks := []health.Check{{Name: "db", Required: true, Run: func(context.Context) error { return nil }}}
	h := health.New(checks, time.Minute)
	h.Refresh(context.Background(), checks, time.Second, nil)
	s, err := Build(&svc.ServiceContext{Config: c, Health: h}, extra...)
	if err != nil {
		t.Fatal(err)
	}
	s.Runtime.Report = func(context.Context, httpapi.Event) {}
	t.Cleanup(func() {
		s.Runtime.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Runtime.Wait(ctx)
		s.Runtime.Close(ctx)
	})
	return s, h
}
func request(t *testing.T, client *http.Client, method, url, body string) (int, map[string]any, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	req.Header.Set("Authorization", "secret-sentinel")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var v map[string]any
	if err = json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	id, _ := v["traceId"].(string)
	if len(id) != 32 || id == "00000000000000000000000000000000" || id == "11111111111111111111111111111111" || id != res.Header.Get(httpapi.TraceHeader) {
		t.Fatalf("bad trace %v", v)
	}
	return res.StatusCode, v, id
}
func TestHTTPExitsAndTrace(t *testing.T) {
	cancelObserved := make(chan struct{})
	s, h := setup(t,
		rest.Route{Method: "GET", Path: "/panic", Handler: func(http.ResponseWriter, *http.Request) { panic("panic-sentinel") }},
		rest.Route{Method: "GET", Path: "/slow", Handler: func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(cancelObserved) }},
		rest.Route{Method: "POST", Path: "/parse", Handler: func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Value int `json:"value"`
			}
			if err := httpx.Parse(r, &req); err != nil {
				httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
				return
			}
			httpapi.JSON(w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
		}},
	)
	var mu sync.Mutex
	events := map[string]httpapi.Event{}
	s.Runtime.Report = func(_ context.Context, e httpapi.Event) { mu.Lock(); defer mu.Unlock(); events[e.TraceID] = e }
	ts := httptest.NewServer(s.HTTP.Handler)
	defer ts.Close()
	for _, tc := range []struct {
		method, path, body, code string
		status                   int
	}{{"GET", "/health/live", "", "OK", 200}, {"GET", "/health/ready", "", "OK", 200}, {"GET", "/missing", "", "NOT_FOUND", 404}, {"POST", "/health/live", "", "METHOD_NOT_ALLOWED", 405}, {"GET", "/panic", "", "INTERNAL_ERROR", 500}, {"POST", "/parse", "{", "INVALID_ARGUMENT", 400}, {"POST", "/parse", strings.Repeat("x", 101), "PAYLOAD_TOO_LARGE", 413}, {"GET", "/slow", "", "REQUEST_TIMEOUT", 503}} {
		t.Run(tc.path+tc.code, func(t *testing.T) {
			status, v, id := request(t, ts.Client(), tc.method, ts.URL+tc.path, tc.body)
			if status != tc.status || v["code"] != tc.code {
				t.Fatalf("%d %v", status, v)
			}
			if status >= 400 {
				if _, ok := v["data"]; ok {
					t.Fatal("error has data")
				}
			}
			mu.Lock()
			event := events[id]
			mu.Unlock()
			if event.TraceID != id || event.Status != status {
				t.Fatalf("missing correlated event: %+v", event)
			}
			if tc.path == "/panic" && (len(event.Stack) == 0 || !strings.Contains(event.Cause.Error(), "panic-sentinel")) {
				t.Fatal("panic diagnostics lost")
			}
		})
	}
	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("timeout did not cancel")
	}
	h.Stop()
	status, _, _ := request(t, ts.Client(), "GET", ts.URL+"/health/ready", "")
	if status != 503 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts.Client(), "GET", ts.URL+"/health/live", "")
	if status != 200 {
		t.Fatal(status)
	}
}
func TestAdmissionAndGracefulShutdown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s, _ := setup(t, rest.Route{Method: "GET", Path: "/work", Handler: func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		httpapi.JSON(w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
	}})
	s.Runtime.Config.RequestTimeout = time.Second
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- s.Serve(ctx, ln, time.Second) }()
	completed := make(chan error, 1)
	url := "http://" + ln.Addr().String()
	go func() {
		resp, err := http.Get(url + "/work")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				err = errors.New("work failed")
			}
		}
		completed <- err
	}()
	<-entered
	status, _, _ := request(t, http.DefaultClient, "GET", url+"/work", "")
	if status != 429 {
		t.Fatal(status)
	}
	cancel()
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if s.Runtime.Health.Ready() {
		t.Fatal("still ready after stop")
	}
}

func TestStructuredDiagnosticsExcludeRequestCredentials(t *testing.T) {
	s, h := setup(t, rest.Route{Method: "POST", Path: "/failure", Handler: func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(r.Context(), w, errors.New("diagnostic-cause"))
	}})
	reporter := httpapi.New(s.Runtime.Config, h)
	defer reporter.Close(context.Background())
	s.Runtime.Report = reporter.Report
	var output bytes.Buffer
	old := logx.Reset()
	defer logx.SetWriter(old)
	logx.SetWriter(logx.NewWriter(&output))
	// Execute synchronously through the same router/middleware to capture the complete log event.
	req := httptest.NewRequest("POST", "/failure", strings.NewReader(`{"password":"body-secret"}`))
	req.Header.Set("Authorization", "header-secret")
	req.Header.Set("Cookie", "cookie-secret")
	w := httptest.NewRecorder()
	s.HTTP.Handler.ServeHTTP(w, req)
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("JSON log: %v %s", err, output.String())
	}
	if event["traceId"] != response["traceId"] || event["trace"] != response["traceId"] || !strings.Contains(output.String(), "diagnostic-cause") {
		t.Fatalf("diagnostic correlation missing: %s", output.String())
	}
	for _, secret := range []string{"body-secret", "header-secret", "cookie-secret"} {
		if strings.Contains(output.String(), secret) || strings.Contains(w.Body.String(), secret) {
			t.Fatal("credential leaked")
		}
	}
	if strings.Contains(w.Body.String(), "diagnostic-cause") {
		t.Fatal("internal cause exposed")
	}
}

func TestShutdownDeadlineBoundsUncooperativeHandler(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s, _ := setup(t, rest.Route{Method: "GET", Path: "/blocked", Handler: func(http.ResponseWriter, *http.Request) { close(entered); <-release }})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln, 20*time.Millisecond) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, _ := http.Get("http://" + ln.Addr().String() + "/blocked")
		if res != nil {
			res.Body.Close()
		}
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked indefinitely")
	}
	close(release)
	<-clientDone
}
