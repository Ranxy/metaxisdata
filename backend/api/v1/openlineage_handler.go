package v1

import (
	"encoding/json"
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
	g.POST("/", h.receiveEvent)
	g.POST("/batch", h.receiveEvent)
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

	// Detect whether the payload is a single event or a batch (JSON array).
	trimmed := bytesTrimLeft(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return h.processBatchEvents(c, body)
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

func (h *OpenLineageHandler) processBatchEvents(c echo.Context, body []byte) error {
	var rawEvents []json.RawMessage
	if err := json.Unmarshal(body, &rawEvents); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to parse event array"})
	}

	var lastErr error
	processed := 0
	for _, raw := range rawEvents {
		event, err := openlineage.ParseRunEvent(raw)
		if err != nil {
			slog.Warn("skipping invalid event in batch", "error", err)
			continue
		}
		if err := h.processor.ProcessRunEvent(c.Request().Context(), event); err != nil {
			slog.Error("failed to process batch event", "runId", event.Run.RunID, "error", err)
			lastErr = err
			continue
		}
		processed++
	}

	if lastErr != nil && processed == 0 {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to process any events"})
	}

	return c.JSON(http.StatusOK, map[string]any{"status": "ok", "processed": processed})
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

// bytesTrimLeft returns body with leading whitespace removed.
func bytesTrimLeft(b []byte) []byte {
	for i := range b {
		if b[i] != ' ' && b[i] != '\t' && b[i] != '\n' && b[i] != '\r' {
			return b[i:]
		}
	}
	return nil
}
