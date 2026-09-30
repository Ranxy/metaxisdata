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

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
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
	store          *store.Store
	processor      *openlineage.Processor
	trustedProxies []string
}

// NewOpenLineageHandler creates a new OpenLineageHandler. trustedProxies is the
// list of peers whose forwarding headers the audit record may believe.
func NewOpenLineageHandler(s *store.Store, trustedProxies []string, lineageAnalyzer *lineage.Analyzer) *OpenLineageHandler {
	return &OpenLineageHandler{
		store:          s,
		processor:      openlineage.NewProcessor(s, lineageAnalyzer),
		trustedProxies: trustedProxies,
	}
}

// RegisterRoutes registers the OpenLineage HTTP routes on the echo instance.
func (h *OpenLineageHandler) RegisterRoutes(g *echo.Group) {
	g.POST("", h.receiveEvent)
	g.POST("/batch", h.receiveEvent)
}

func (h *OpenLineageHandler) receiveEvent(c echo.Context) error {
	started := time.Now()

	// Validate API key.
	apiKey := ExtractIngestionKey(c.Request())
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing or invalid Authorization header"})
	}

	keyMessage, err := h.store.ValidateOpenLineageAPIKey(c.Request().Context(), apiKey)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
	}

	// Ingestion is a plain Echo route and never reaches the Connect audit
	// interceptor, so the handler records the request itself.
	err = h.handleIngestion(c, keyMessage)
	h.auditIngestion(c.Request().Context(), c, keyMessage, started, c.Response().Status)
	return err
}

// handleIngestion reads, validates and persists one ingestion request.
func (h *OpenLineageHandler) handleIngestion(c echo.Context, keyMessage *store.OpenLineageAPIKeyMessage) error {
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

	persistedRun, err := h.store.UpsertOpenLineageRun(c.Request().Context(), h.runMessageForEvent(event))
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
	for i, event := range events {
		runs[i] = h.runMessageForEvent(event)
	}

	// One transaction for the whole batch: a per-event transaction was the
	// dominant cost of ingesting a batch.
	persisted, err := h.store.UpsertOpenLineageRuns(ctx, runs)
	if err != nil {
		slog.Error("failed to persist batch events", "events", len(runs), "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"status":    "error",
			"error":     err.Error(),
			"processed": 0,
			"failed":    invalid + len(events),
		})
	}
	runs = persisted

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

// runMessageForEvent derives the row an event writes. Every run state is stored,
// because a row is the run's latest known state: a job that is still running or
// that failed stays visible instead of leaving no trace at all, and the
// event-type filter has more than one answer to offer. Lineage itself is still
// only derived from a COMPLETE event, which is where it is final.
func (*OpenLineageHandler) runMessageForEvent(event *openlineage.RunEvent) *store.OpenLineageRunMessage {
	derived := openlineage.DeriveRunMetadata(event)

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
		GUID:               openlineage.BuildOpenLineageRunGUID(event.Job.Namespace, event.Job.Name, derived.JobType, event.Run.RunID),
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
	}
}

// ExtractIngestionKey returns the Bearer token of an ingestion request. The
// server rate limiter uses the same parsing, so the limit key matches the key
// the handler authenticates.
func ExtractIngestionKey(r *http.Request) string {
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

// auditIngestion records one ingestion request. A failure to write the audit row
// must not fail the ingestion itself, so it is only logged.
func (h *OpenLineageHandler) auditIngestion(ctx context.Context, c echo.Context, key *store.OpenLineageAPIKeyMessage, started time.Time, status int) {
	auditCtx, cancel := audit.Context(ctx)
	defer cancel()

	workspaceID, err := h.store.GetWorkspaceID(auditCtx)
	if err != nil {
		slog.Error("failed to resolve the workspace for the ingestion audit log", clog.WithError(err))
		return
	}

	actor := key.CreatedBy
	if actor == "" {
		actor = common.FormatAPIKey(key.ID)
	}

	auditErr := audit.ErrorForHTTPStatus(status)
	auditLog := &storepb.AuditLog{
		Parent:          common.FormatWorkspace(workspaceID),
		Method:          c.Request().URL.Path,
		Resource:        common.FormatAPIKey(key.ID),
		User:            actor,
		Severity:        audit.MapSeverity(auditErr),
		Status:          audit.BuildAuditStatus(auditErr),
		LatencyMs:       time.Since(started).Milliseconds(),
		RequestMetadata: audit.BuildRequestMetadata(c.Request().Header, c.Request().RemoteAddr, h.trustedProxies),
	}
	if _, createErr := h.store.CreateAuditLog(auditCtx, auditLog); createErr != nil {
		slog.Error("failed to persist the ingestion audit log", clog.WithError(createErr))
	}
}
