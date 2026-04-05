package v1

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const maxOpenLineageBodySize = 10 * 1024 * 1024 // 10MB

// OpenLineageHandler handles OpenLineage event ingestion via HTTP.
type OpenLineageHandler struct {
	store     *store.Store
	processor *openlineage.Processor
}

// NewOpenLineageHandler creates a new OpenLineageHandler.
func NewOpenLineageHandler(s *store.Store) *OpenLineageHandler {
	return &OpenLineageHandler{
		store:     s,
		processor: openlineage.NewProcessor(s),
	}
}

// RegisterRoutes registers the OpenLineage HTTP routes on the echo instance.
func (h *OpenLineageHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/events", h.receiveEvent)
}

func (h *OpenLineageHandler) receiveEvent(c echo.Context) error {
	// Validate API key.
	apiKey := extractBearerToken(c.Request())
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing or invalid Authorization header"})
	}

	if _, err := h.store.ValidateOpenLineageAPIKey(c.Request().Context(), apiKey); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
	}

	// Read body with size limit.
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxOpenLineageBodySize))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
	}

	event, err := openlineage.ParseRunEvent(body)
	if err != nil {
		slog.Warn("invalid OpenLineage event", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if err := h.processor.ProcessRunEvent(c.Request().Context(), event); err != nil {
		slog.Error("failed to process OpenLineage event", "runId", event.Run.RunID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to process event"})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}
