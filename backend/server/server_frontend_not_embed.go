package server

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

func embedFrontend(e *echo.Echo) {
	slog.Info("This build does not bundle the frontend; serve frontend/dist separately.")

	e.GET("/*", func(c echo.Context) error {
		return c.HTML(http.StatusOK, "This server build does not bundle frontend and backend together.")
	})
}
