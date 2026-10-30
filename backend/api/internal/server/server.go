package server

import (
	"context"
	"net/http"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/handler"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/middleware"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/rest"
	framework "github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/router"
)

// Build configures the native server. Project middleware only covers the JSON
// contract, public trace boundary and dependency/work admission policy.
func Build(s *svc.ServiceContext) (*rest.Server, *httpapi.Runtime, error) {
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
		return nil, nil, err
	}
	// The default 404 wrapper starts a second trace. Keep our common outer trace.
	fallback := func(code apperror.Code) http.Handler {
		return framework.TimeoutHandler(s.Config.RequestDuration())(rt.Work(func(w http.ResponseWriter, r *http.Request) { httpapi.Error(r.Context(), w, apperror.New(code, nil)) }))
	}
	r.SetNotFoundHandler(fallback(apperror.NotFound))
	r.SetNotAllowedHandler(fallback(apperror.MethodNotAllowed))
	app.Use(rt.Work)
	s.AuthTransport = middleware.NewAuthTransportMiddleware(s).Handle
	s.UserAuth = middleware.NewUserAuthMiddleware(s).Handle
	s.IdentityAuth = middleware.NewIdentityAuthMiddleware(s).Handle
	s.BackofficeTransport = middleware.NewBackofficeTransportMiddleware(s).Handle
	s.AdminAuth = middleware.NewAdminAuthMiddleware(s).Handle
	s.MerchantAuth = middleware.NewMerchantAuthMiddleware(s).Handle
	handler.RegisterHandlers(app, s)
	return app, rt, nil
}

// Start leaves listening, signals, HTTP shutdown and the process deadline to
// go-zero. Its starter waits for ALL shutdown listeners before returning, so the
// caller can close dependencies afterwards without racing active handlers.
func Start(app *rest.Server, rt *httpapi.Runtime) (drainErr error) {
	proc.AddWrapUpListener(rt.Stop)
	proc.AddShutdownListener(func() {
		// Wrap-up listeners run concurrently; also close admission here before Wait.
		rt.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), rt.Config.ShutdownTimeout)
		defer cancel()
		drainErr = rt.Wait(ctx)
		if drainErr != nil {
			logx.Errorw("request work did not finish before shutdown deadline", logx.Field("cause", drainErr.Error()))
		}
	})
	app.StartWithOpts(func(s *http.Server) {
		s.Handler = rt.Middleware(s.Handler)
	})
	return drainErr
}

// Decorate matches via the public Router interface; generated routes stay untouched.
type routeRouter struct{ httpx.Router }

func (r *routeRouter) Handle(method, path string, h http.Handler) error {
	return r.Router.Handle(method, path, httpapi.Route(path, h))
}
