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
	"github.com/zeromicro/go-zero/rest/chain"
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
	r := router.NewRouter()
	c := rest.RestConf{}
	c.Name = s.Config.Name
	c.Host = s.Config.Host
	c.Port = s.Config.Port
	c.Log = s.Config.Log
	c.Shutdown.WaitTime = s.Config.ShutdownTimeout + 2*s.Config.DependencyTimeout + time.Second
	app, err := rest.NewServer(c, rest.WithRouter(r), rest.WithChain(chain.New()))
	if err != nil {
		return nil, err
	}
	// Override the router directly: the framework default 404 wrapper starts a second trace.
	r.SetNotFoundHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(r.Context(), w, apperror.New(apperror.NotFound, nil))
	}))
	r.SetNotAllowedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(r.Context(), w, apperror.New(apperror.MethodNotAllowed, nil))
	}))
	handler.RegisterHandlers(app, s)
	for _, route := range extra {
		app.AddRoute(route)
	} // Test harness only; main passes no extras.
	for _, route := range app.Routes() {
		rt.Routes[route.Method+" "+route.Path] = route.Path
	}
	adapter, err := rest.NewServerless(app)
	if err != nil {
		return nil, err
	}
	return &Server{Runtime: rt, HTTP: &http.Server{Handler: rt.Middleware(http.HandlerFunc(adapter.Serve)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: s.Config.RequestTimeout, WriteTimeout: s.Config.RequestTimeout + time.Second, IdleTimeout: 60 * time.Second}}, nil
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
	return errors.Join(shutdownErr, workErr, s.Runtime.Close(drain))
}
