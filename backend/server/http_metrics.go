package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// httpMetricsMiddleware records every completed HTTP request. It runs on the API
// port only: the Prometheus endpoint lives on its own listener, and the scrape
// itself is never counted.
func httpMetricsMiddleware(m *metrics.Metrics) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			request := c.Request()
			if skipHTTPMetrics(request) {
				return next(c)
			}
			started := time.Now()
			method := request.Method
			route := routeLabel(c)
			// The recover middleware is installed outside this one, so a panicking
			// handler unwinds past the normal return. Record it as a 500 here before
			// letting the panic continue to that boundary.
			defer func() {
				if recovered := recover(); recovered != nil {
					m.ObserveHTTPRequest(method, route, http.StatusInternalServerError, time.Since(started))
					panic(recovered)
				}
			}()
			err := next(c)

			status := c.Response().Status
			if err != nil {
				var httpError *echo.HTTPError
				if errors.As(err, &httpError) {
					status = httpError.Code
				}
				if status == 0 || status == http.StatusOK {
					status = http.StatusInternalServerError
				}
			}
			m.ObserveHTTPRequest(method, route, status, time.Since(started))
			return err
		}
	}
}

// skipHTTPMetrics drops traffic that is not API work or would create meaningless
// series: health checks, pprof sessions and the REST gateway's loopback hop.
func skipHTTPMetrics(request *http.Request) bool {
	switch path := request.URL.Path; {
	case path == "/healthz", path == "/metrics":
		return true
	case path == "/debug/pprof", strings.HasPrefix(path, "/debug/pprof/"):
		return true
	default:
		return audit.IsGatewayForward(request)
	}
}

// routeLabel turns the Echo route template into a bounded label value.
func routeLabel(c echo.Context) string {
	switch route := c.Path(); route {
	case "":
		return "unmatched"
	case "/*":
		return "spa"
	default:
		return route
	}
}
