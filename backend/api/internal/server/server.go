package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/handler"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/zeromicro/go-zero/rest"
	framework "github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/router"
)

type Server struct {
	HTTP    *http.Server
	Runtime *httpapi.Runtime
}

// Build preserves goctl route registration and go-zero routing. net/http owns
// listening/shutdown so readiness, in-flight work and dependency close are ordered.
func Build(s *svc.ServiceContext, extra ...rest.Route) (*Server, error) {
	rt := httpapi.New(s.Config, s.Health)
	httpapi.ConfigureErrors()
	r := &routeRouter{Router: router.NewRouter()}
	c := s.Config.RestConf
	// Keep native Timeout/Metrics/Prometheus. Move tracing outside the router for
	// 404/405; project adapters cover JSON errors, safe logging and global work slots.
	c.Middlewares.Trace = false
	c.Middlewares.Log = false
	c.Middlewares.Recover = false
	c.Middlewares.MaxConns = false
	c.Middlewares.MaxBytes = false
	app, err := rest.NewServer(c, rest.WithRouter(r))
	if err != nil {
		return nil, err
	}
	// The default 404 wrapper starts a second trace. Keep our common outer trace.
	fallback := func(code apperror.Code) http.Handler {
		return framework.TimeoutHandler(s.Config.RequestDuration())(rt.Work(func(w http.ResponseWriter, r *http.Request) { httpapi.Error(r.Context(), w, apperror.New(code, nil)) }))
	}
	r.SetNotFoundHandler(fallback(apperror.NotFound))
	r.SetNotAllowedHandler(fallback(apperror.MethodNotAllowed))
	app.Use(rt.Work)
	handler.RegisterHandlers(app, s)
	for _, route := range extra {
		app.AddRoute(route)
	} // Test harness only; main passes no extras.
	adapter, err := rest.NewServerless(app)
	if err != nil {
		return nil, err
	}
	return &Server{Runtime: rt, HTTP: &http.Server{Handler: rt.Middleware(http.HandlerFunc(adapter.Serve)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: s.Config.RequestDuration(), WriteTimeout: s.Config.RequestDuration() + time.Second, IdleTimeout: 60 * time.Second}}, nil
}
func (s *Server) Serve(ctx context.Context, listener net.Listener, grace time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- s.HTTP.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	s.Runtime.Stop()
	drain, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	shutdownErr := s.HTTP.Shutdown(drain)
	if shutdownErr != nil {
		_ = s.HTTP.Close()
	}
	workErr := s.Runtime.Wait(drain)
	return errors.Join(shutdownErr, workErr)
}

// Decorate matches via the public Router interface; generated routes stay untouched.
type routeRouter struct{ httpx.Router }

func (r *routeRouter) Handle(method, path string, h http.Handler) error {
	return r.Router.Handle(method, path, httpapi.Route(path, h))
}
