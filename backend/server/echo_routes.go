package server

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo-contrib/prometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/config"

	connectcors "connectrpc.com/cors"
)

func configureEchoRouters(
	e *echo.Echo,
	profile *config.Profile,
) {
	e.Use(recoverMiddleware)

	if profile.Mode == common.ReleaseModeDev {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOriginFunc: func(string) (bool, error) {
				return true, nil
			},
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
			AllowHeaders:     connectcors.AllowedHeaders(),
			ExposeHeaders:    connectcors.ExposedHeaders(),
			AllowCredentials: true,
		}))
	}

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

	p := prometheus.NewPrometheus("api", nil)
	p.RequestCounterURLLabelMappingFunc = func(c echo.Context) string {
		return c.Request().URL.Path
	}
	p.Use(e)

	e.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
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
