//go:build embed_frontend

package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/Ranxy/metaxisdata/backend/common/log"
)

// frontendDist holds the built SPA. `make build-embed` copies frontend/dist
// here before compiling; the committed .gitkeep keeps the embed pattern
// resolvable so the tagged build still compiles without a frontend build.
//
//go:embed all:frontend_dist
var frontendDist embed.FS

// embedFrontend serves the bundled SPA. Unknown paths fall back to index.html
// so a full page load of a client-side route still boots the app.
func embedFrontend(e *echo.Echo) {
	sub, err := fs.Sub(frontendDist, "frontend_dist")
	if err != nil {
		slog.Error("failed to open the embedded frontend", log.WithError(err))
		return
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		slog.Warn("built with the embed_frontend tag but no frontend was bundled; run `make build-embed`")
	}

	fileServer := http.FileServer(http.FS(sub))
	e.GET("/*", func(c echo.Context) error {
		requestPath := strings.TrimPrefix(c.Request().URL.Path, "/")
		if requestPath == "" {
			requestPath = "index.html"
		}
		if _, err := fs.Stat(sub, requestPath); err != nil {
			// Not a file we shipped: treat it as a client-side route.
			c.Request().URL.Path = "/"
		}
		fileServer.ServeHTTP(c.Response(), c.Request())
		return nil
	})
}
