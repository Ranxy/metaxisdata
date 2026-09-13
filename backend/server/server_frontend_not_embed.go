//go:build !embed_frontend

package server

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// embedFrontend is the default build's SPA handler. Without the embed_frontend
// tag the binary carries no frontend: serve the dev server or a separately
// hosted frontend/dist. Use `make build-embed` for a self-contained binary.
func embedFrontend(e *echo.Echo) {
	slog.Info("This build does not bundle the frontend; serve frontend/dist separately or build with `make build-embed`.")

	e.GET("/*", func(c echo.Context) error {
		return c.HTML(http.StatusOK, "This server build does not bundle frontend and backend together.")
	})
}
