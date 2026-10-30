package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// Serverless is only a test adapter: production starts the native rest.Server.
type testServer struct {
	HTTP    *http.Server
	Runtime *httpapi.Runtime
}

func setup(t *testing.T, extra ...rest.Route) (*testServer, *health.Monitor) {
	t.Helper()
	t.Setenv("DEXWALLET_ID_SECRET", "test-only-key123")
	t.Setenv("DEXWALLET_MYSQL_DSN", "test@tcp(localhost:3306)/test")
	c, err := config.Load("../../etc/platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Timeout = 100
	c.MaxConns = 1
	c.MaxBytes = 100
	checks := []health.Check{{Name: "db", Required: true, Run: func(context.Context) error { return nil }}}
	h := health.New(checks, time.Minute)
	h.Refresh(context.Background(), checks, time.Second, nil)
	app, rt, err := Build(&svc.ServiceContext{Config: c, Health: h})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range extra {
		app.AddRoute(route)
	}
	adapter, err := rest.NewServerless(app)
	if err != nil {
		t.Fatal(err)
	}
	s := &testServer{HTTP: &http.Server{Handler: rt.Middleware(http.HandlerFunc(adapter.Serve))}, Runtime: rt}
	s.Runtime.Report = func(context.Context, httpapi.Event) {}
	t.Cleanup(func() {
		s.Runtime.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Runtime.Wait(ctx)
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
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed {
		body, err := io.ReadAll(res.Body)
		if err != nil || len(body) == 0 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
			t.Fatal("standard HTTP fallback", err, res.Header)
		}
		return res.StatusCode, nil, res.Header.Get(httpapi.TraceHeader)
	}
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
			httpapi.JSON(r.Context(), w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
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
	}{{"GET", "/health/live", "", "OK", 200}, {"GET", "/health/ready", "", "OK", 200}, {"GET", "/missing", "", "", 404}, {"POST", "/v1/user/auth/logout-all", "", "", 404}, {"POST", "/health/live", "", "", 405}, {"GET", "/panic", "", "INTERNAL_ERROR", 500}, {"POST", "/parse", "{", "INVALID_ARGUMENT", 400}, {"POST", "/parse", strings.Repeat("x", 101), "PAYLOAD_TOO_LARGE", 413}, {"GET", "/slow", "", "REQUEST_TIMEOUT", 503}} {
		t.Run(tc.path+tc.code, func(t *testing.T) {
			status, v, id := request(t, ts.Client(), tc.method, ts.URL+tc.path, tc.body)
			if status != tc.status || (tc.code != "" && v["code"] != tc.code) {
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
func TestAdmissionKeepsHealthAvailable(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s, _ := setup(t, rest.Route{Method: "GET", Path: "/work", Handler: func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		httpapi.JSON(r.Context(), w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
	}})
	ts := httptest.NewServer(s.HTTP.Handler)
	defer ts.Close()
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		res, err := ts.Client().Get(ts.URL + "/work")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	<-entered
	status, _, _ := request(t, ts.Client(), "GET", ts.URL+"/work", "")
	if status != 429 {
		t.Fatal(status)
	}
	status, _, _ = request(t, ts.Client(), "GET", ts.URL+"/health/live", "")
	if status != 200 {
		t.Fatal(status)
	}
	once.Do(func() { close(release) })
	<-completed
}

func TestStructuredDiagnosticsExcludeRequestCredentials(t *testing.T) {
	s, h := setup(t, rest.Route{Method: "POST", Path: "/failure", Handler: func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(r.Context(), w, errors.New("diagnostic-cause"))
	}})
	reporter := httpapi.New(s.Runtime.Config, h)
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

// The timeout worker remains subject to admission and drain even after its HTTP
// response has returned. Its late failure must retain the original correlation.
func TestTimedOutWorkRetainsSlotAndDiagnostics(t *testing.T) {
	lateWrite := make(chan error, 1)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s, _ := setup(t, rest.Route{Method: "GET", Path: "/late", Handler: func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, err := w.Write([]byte("late-response"))
		lateWrite <- err
		panic("late-panic")
	}})
	events := make(chan httpapi.Event, 8)
	s.Runtime.Report = func(_ context.Context, e httpapi.Event) { events <- e }
	w := httptest.NewRecorder()
	s.HTTP.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/late", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "REQUEST_TIMEOUT") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	first := <-events
	w2 := httptest.NewRecorder()
	s.HTTP.Handler.ServeHTTP(w2, httptest.NewRequest("GET", "/late", nil))
	if w2.Code != 429 {
		t.Fatalf("timed-out work released admission: %d", w2.Code)
	}
	<-events
	s.Runtime.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Runtime.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker not drained: %v", err)
	}
	once.Do(func() { close(release) })
	select {
	case late := <-events:
		if late.TraceID != first.TraceID || len(late.Stack) == 0 || !strings.Contains(late.Cause.Error(), "late-panic") {
			t.Fatalf("late diagnostic: %+v", late)
		}
	case <-time.After(time.Second):
		t.Fatal("late diagnostic missing")
	}
	if err := <-lateWrite; !errors.Is(err, http.ErrHandlerTimeout) {
		t.Fatalf("late write: %v", err)
	}
	if !strings.Contains(w.Body.String(), "REQUEST_TIMEOUT") || strings.Contains(w.Body.String(), "late-panic") {
		t.Fatal("late failure replaced timeout response")
	}
}

func TestFrameworkAdaptersPreserveJSONContract(t *testing.T) {
	s, _ := setup(t,
		rest.Route{Method: "POST", Path: "/body", Handler: func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err != nil {
				httpapi.Error(r.Context(), w, apperror.New(apperror.InvalidArgument, err))
			}
		}},
		rest.Route{Method: "GET", Path: "/auth", Handler: func(w http.ResponseWriter, r *http.Request) {
			httpapi.Error(r.Context(), w, apperror.New(apperror.Unauthenticated, nil))
		}},
		rest.Route{Method: "GET", Path: "/forbidden", Handler: func(w http.ResponseWriter, r *http.Request) {
			httpapi.Error(r.Context(), w, apperror.New(apperror.PermissionDenied, nil))
		}},
		rest.Route{Method: "GET", Path: "/items/:idx", Handler: func(w http.ResponseWriter, r *http.Request) {
			httpapi.JSON(r.Context(), w, 200, map[string]any{"code": "OK", "traceId": httpapi.TraceID(r.Context())})
		}},
		rest.Route{Method: "GET", Path: "/cancel", Handler: func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }},
	)
	for _, tc := range []struct {
		name, method, path, code string
		status                   int
		header, value            string
	}{
		{name: "unknown body size", method: "POST", path: "/body", code: "PAYLOAD_TOO_LARGE", status: 413},
		{name: "unauthenticated", method: "GET", path: "/auth", code: "UNAUTHENTICATED", status: 401},
		{name: "forbidden", method: "GET", path: "/forbidden", code: "PERMISSION_DENIED", status: 403},
		{name: "dynamic route", method: "GET", path: "/items/123", code: "OK", status: 200},
		{name: "SSE cannot bypass timeout", method: "GET", path: "/cancel", code: "INVALID_ARGUMENT", status: 400, header: "Accept", value: "text/event-stream"},
		{name: "upgrade cannot bypass timeout", method: "GET", path: "/cancel", code: "INVALID_ARGUMENT", status: 400, header: "Upgrade", value: "websocket"},
		{name: "client cancellation", method: "GET", path: "/cancel", code: "REQUEST_TIMEOUT", status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan httpapi.Event, 2)
			s.Runtime.Report = func(_ context.Context, e httpapi.Event) { events <- e }
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.method == "POST" {
				req.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 101)))
				req.ContentLength = -1
			}
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			if tc.name == "client cancellation" {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			s.HTTP.Handler.ServeHTTP(w, req)
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body["code"] != tc.code || body["traceId"] != w.Header().Get(httpapi.TraceHeader) {
				t.Fatalf("%d %v", w.Code, body)
			}
			if (w.Header().Get("WWW-Authenticate") != "") != (tc.status == 401) {
				t.Fatal("incorrect authentication challenge")
			}
			event := <-events
			if tc.path == "/items/123" && event.Route != "/items/:idx" {
				t.Fatalf("route template lost: %+v", event)
			}
		})
	}
}

func TestPanicAfterWriteAbortsInsteadOfReturningSuccess(t *testing.T) {
	s, _ := setup(t, rest.Route{Method: "GET", Path: "/partial", Handler: func(w http.ResponseWriter, r *http.Request) {
		httpapi.JSON(r.Context(), w, 200, map[string]any{"code": "OK"})
		panic("after-write")
	}})
	events := make(chan httpapi.Event, 1)
	s.Runtime.Report = func(_ context.Context, e httpapi.Event) { events <- e }
	ts := httptest.NewServer(s.HTTP.Handler)
	defer ts.Close()
	res, err := ts.Client().Get(ts.URL + "/partial")
	if res != nil {
		res.Body.Close()
	}
	if err == nil {
		t.Fatal("partially written panic returned a normal response")
	}
	select {
	case event := <-events:
		if event.Status != 500 || len(event.Stack) == 0 || !strings.Contains(event.Cause.Error(), "after-write") {
			t.Fatalf("panic diagnostic lost: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("panic diagnostic missing")
	}
}
