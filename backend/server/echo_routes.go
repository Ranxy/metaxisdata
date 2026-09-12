package server

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/config"

	connectcors "connectrpc.com/cors"
)

func configureEchoRouters(
	e *echo.Echo,
	profile *config.Profile,
) {
	e.Use(recoverMiddleware)

	// Cap the request body. The REST gateway allowed up to 100MB to be received
	// but never bounded what a client could send.
	e.Use(middleware.BodyLimit("100M"))

	// CORS is installed only for explicitly configured origins. A credentialed
	// CORS policy that echoes any origin lets any website issue authenticated
	// requests, so the wide-open dev default is gone.
	if len(profile.CORSAllowOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:     profile.CORSAllowOrigins,
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
			AllowHeaders:     connectcors.AllowedHeaders(),
			ExposeHeaders:    connectcors.ExposedHeaders(),
			AllowCredentials: true,
		}))
	}

	// Defense in depth for the cookie-based web login: a state-changing request
	// that rides a session cookie must come from a trusted origin.
	e.Use(csrfProtectionMiddleware(profile))

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogMethod: true,
		LogStatus: true,
		LogError:  true,
		LogValuesFunc: func(_ echo.Context, values middleware.RequestLoggerValues) error {
			if values.Error != nil {
				slog.Error("echo request logger", "method", values.Method, "uri", values.URI, "status", values.Status, log.WithError(values.Error))
			}
			return nil
		},
	}))

	// TODO we need to Embed frontend at future. for now, we just use frontend not embed for skip this
	embedFrontend(e)

	e.HideBanner = true
	e.HidePort = true

	registerPprof(e, &profile.RuntimeDebug)

	// /metrics exposes route-level request counts to anyone who can reach the
	// server, so it follows the same runtime-debug gate as pprof.
	e.Use(metricsGateMiddleware(&profile.RuntimeDebug))
	p := prometheus.NewPrometheus("api", nil)
	p.RequestCounterURLLabelMappingFunc = func(c echo.Context) string {
		return c.Request().URL.Path
	}
	p.Use(e)

	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
}

// metricsGateMiddleware hides the Prometheus endpoint unless runtime debug is
// enabled. It is checked per request, so the admin setting takes effect without
// a restart.
func metricsGateMiddleware(runtimeDebug *atomic.Bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().URL.Path == "/metrics" && !runtimeDebug.Load() {
				return echo.ErrNotFound
			}
			return next(c)
		}
	}
}

func recoverMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = errors.Errorf("%v", r)
				}
				slog.Error("Middleware PANIC RECOVER", log.WithError(err), log.Stack("panic-stack"))

				c.Error(err)
			}
		}()
		return next(c)
	}
}
