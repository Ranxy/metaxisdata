package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	maxOpenLineageBodySize = 8 << 20 // 8MiB
	// maxOpenLineageBatchEvents bounds the work one request may ask for. The
	// body limit alone is not enough: a minimal event is well under 100 bytes,
	// so a single 8MiB body can still carry tens of thousands of them.
	maxOpenLineageBatchEvents = 1000
)

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
	g.POST("", h.receiveEvent)
	g.POST("/batch", h.receiveEvent)
}

func (h *OpenLineageHandler) receiveEvent(c echo.Context) error {
	// Validate API key.
	apiKey := extractBearerToken(c.Request())
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing or invalid Authorization header"})
	}

	keyMessage, err := h.store.ValidateOpenLineageAPIKey(c.Request().Context(), apiKey)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
	}

	// Read body with size limit.
	body, tooLarge, err := readBodyLimited(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
	}
	if tooLarge {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"error": fmt.Sprintf("request body exceeds %d bytes", maxOpenLineageBodySize),
			"limit": maxOpenLineageBodySize,
		})
	}

	// Detect whether the payload is a single event or a batch (JSON array).
	trimmed := bytes.TrimLeft(body, " \t\n\r")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return h.processBatchEvents(c, body, keyMessage.ScopeNamespace)
	}

	event, err := openlineage.ParseRunEvent(body)
	if err != nil {
		slog.Warn("invalid OpenLineage event", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if !eventWithinScope(event, keyMessage.ScopeNamespace) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "API key is not scoped to this OpenLineage namespace"})
	}

	persistedRun, err := h.persistEvent(c.Request().Context(), event)
	if err != nil {
		slog.Error("failed to persist OpenLineage event", "runId", event.Run.RunID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist event"})
	}

	if err := h.processor.ProcessRunEvent(c.Request().Context(), event, persistedRun); err != nil {
		slog.Error("failed to process OpenLineage event", "runId", event.Run.RunID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to process event"})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpenLineageHandler) processBatchEvents(c echo.Context, body []byte, scope string) error {
	var rawEvents []json.RawMessage
	if err := json.Unmarshal(body, &rawEvents); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to parse event array"})
	}
	if len(rawEvents) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "empty event array"})
	}
	if len(rawEvents) > maxOpenLineageBatchEvents {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
			"error": fmt.Sprintf("batch exceeds %d events", maxOpenLineageBatchEvents),
			"limit": maxOpenLineageBatchEvents,
		})
	}

	// Parse everything before writing anything, so a scoped key is rejected as a
	// whole request rather than half-applied.
	var events []*openlineage.RunEvent
	invalid := 0
	var firstErr error
	for i, raw := range rawEvents {
		event, err := openlineage.ParseRunEvent(raw)
		if err != nil {
			slog.Warn("skipping invalid event in batch", "index", i, "error", err)
			invalid++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !eventWithinScope(event, scope) {
			return c.JSON(http.StatusForbidden, map[string]any{
				"error": "API key is not scoped to this OpenLineage namespace",
				"index": i,
			})
		}
		events = append(events, event)
	}

	ctx := c.Request().Context()
	runs := make([]*store.OpenLineageRunMessage, len(events))
	toPersist := make([]*store.OpenLineageRunMessage, 0, len(events))
	persistIdx := make([]int, 0, len(events))
	for i, event := range events {
		run, needsPersist := h.runMessageForEvent(event)
		runs[i] = run
		if needsPersist {
			toPersist = append(toPersist, run)
			persistIdx = append(persistIdx, i)
		}
	}

	// One transaction for the whole batch: a per-event transaction was the
	// dominant cost of ingesting a batch.
	persisted, err := h.store.UpsertOpenLineageRuns(ctx, toPersist)
	if err != nil {
		slog.Error("failed to persist batch events", "events", len(toPersist), "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"status":    "error",
			"error":     err.Error(),
			"processed": 0,
			"failed":    invalid + len(events),
		})
	}
	for i, idx := range persistIdx {
		runs[idx] = persisted[i]
	}

	processed, failed := 0, 0
	for i, event := range events {
		if err := h.processor.ProcessRunEvent(ctx, event, runs[i]); err != nil {
			slog.Error("failed to process batch event", "index", i, "runId", event.Run.RunID, "error", err)
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		processed++
	}

	// The old handler answered 200 as long as one event succeeded, so dropped
	// events were invisible and the producer never retried them.
	if invalid+failed == 0 {
		return c.JSON(http.StatusOK, map[string]any{"status": "ok", "processed": processed, "failed": 0})
	}
	if processed == 0 && failed == 0 {
		// Every event was unparseable: resending the same body cannot help.
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":     "no event in the batch could be parsed",
			"processed": 0,
			"failed":    invalid,
		})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{
		"status":    "error",
		"error":     firstErr.Error(),
		"processed": processed,
		"failed":    invalid + failed,
	})
}

// readBodyLimited reads at most maxOpenLineageBodySize bytes and reports whether
// the body was larger, so an oversized request is refused instead of being
// silently truncated into a parse error.
func readBodyLimited(r io.Reader) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxOpenLineageBodySize+1))
	if err != nil {
		return nil, false, err
	}
	if len(body) > maxOpenLineageBodySize {
		return nil, true, nil
	}
	return body, false, nil
}

// eventWithinScope reports whether a scoped key may submit this event. A key is
// scoped to one OpenLineage namespace, and every namespace in the event -- the
// job plus each input and output dataset -- must match it.
func eventWithinScope(event *openlineage.RunEvent, scope string) bool {
	if scope == "" {
		return true
	}
	if event.Job.Namespace != scope {
		return false
	}
	for _, dataset := range event.Inputs {
		if dataset.Namespace != scope {
			return false
		}
	}
	for _, dataset := range event.Outputs {
		if dataset.Namespace != scope {
			return false
		}
	}
	return true
}

// runMessageForEvent derives the run for an event. Only a COMPLETE event is
// written; anything else yields the identity the processor needs without a
// database write, which is what the second result reports.
// parseEventTime parses an OpenLineage eventTime. The spec requires an RFC3339
// offset, but producers sometimes omit it; assuming UTC keeps the event's
// ordering and retention behavior instead of storing a NULL that sorts last and
// is exempt from pruning.
func parseEventTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, raw+"Z")
		if err != nil {
			return time.Time{}, err
		}
	}
	return parsed.UTC(), nil
}

func (*OpenLineageHandler) runMessageForEvent(event *openlineage.RunEvent) (*store.OpenLineageRunMessage, bool) {
	derived := openlineage.DeriveRunMetadata(event)
	guid := openlineage.BuildOpenLineageRunGUID(event.Job.Namespace, event.Job.Name, derived.JobType, event.Run.RunID)
	if event.EventType != "COMPLETE" {
		return &store.OpenLineageRunMessage{
			GUID:     guid,
			TaskGUID: derived.TaskGUID,
		}, false
	}

	var eventTime *time.Time
	if raw := strings.TrimSpace(event.EventTime); raw != "" {
		parsedTime, err := parseEventTime(raw)
		if err != nil {
			slog.Warn("failed to parse OpenLineage event time", "eventTime", event.EventTime, "runId", event.Run.RunID, "error", err)
		} else {
			eventTime = &parsedTime
		}
	}

	return &store.OpenLineageRunMessage{
		GUID:               guid,
		TaskGUID:           derived.TaskGUID,
		RunID:              event.Run.RunID,
		JobNamespace:       event.Job.Namespace,
		JobName:            event.Job.Name,
		JobType:            derived.JobType,
		EventType:          event.EventType,
		EventTime:          eventTime,
		Producer:           event.Producer,
		Integration:        derived.Integration,
		ProcessingType:     derived.ProcessingType,
		ParentJobNamespace: derived.ParentJobNamespace,
		ParentJobName:      derived.ParentJobName,
		ParentRunID:        derived.ParentRunID,
		RootJobNamespace:   derived.RootJobNamespace,
		RootJobName:        derived.RootJobName,
		RootRunID:          derived.RootRunID,
		Source:             "openlineage",
		InputCount:         int32(len(event.Inputs)),
		OutputCount:        int32(len(event.Outputs)),
		HasLineage:         derived.HasLineage,
		RawPayload:         event.RawJSON,
	}, true
}

func (h *OpenLineageHandler) persistEvent(ctx context.Context, event *openlineage.RunEvent) (*store.OpenLineageRunMessage, error) {
	run, needsPersist := h.runMessageForEvent(event)
	if !needsPersist {
		return run, nil
	}
	return h.store.UpsertOpenLineageRun(ctx, run)
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
