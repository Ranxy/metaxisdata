package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"

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
	// maxOpenLineageSchemaFacetBytes bounds the schema facet stored with one
	// dataset reference, and maxOpenLineageColumnLineageFieldsBytes bounds the
	// column names stored with it. The dataset detail reads (and detoasts) the
	// facets of the references it considers, so one facet must not be able to
	// make that read unbounded. A facet past its cap keeps the entries that fit.
	maxOpenLineageSchemaFacetBytes         = 64 << 10
	maxOpenLineageColumnLineageFieldsBytes = 32 << 10
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
	if err := openlineage.ValidateEventLimits(event); err != nil {
		slog.Warn("OpenLineage event exceeds the ingestion limits", "error", err)
		if errors.Is(err, openlineage.ErrEventTooLarge) {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
				"error": err.Error(),
				"limit": openlineage.MaxEventSize,
			})
		}
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
	// An event over the per-event size limit refuses the whole batch, the same
	// way an over-limit body does: the size is a property of the transport, not
	// of the event's validity.
	for i, raw := range rawEvents {
		if len(raw) > openlineage.MaxEventSize {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]any{
				"error": fmt.Sprintf("event %d exceeds %d bytes", i, openlineage.MaxEventSize),
				"index": i,
				"limit": openlineage.MaxEventSize,
			})
		}
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
		if err := openlineage.ValidateEventLimits(event); err != nil {
			slog.Warn("skipping oversized event in batch", "index", i, "error", err)
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

	// Every event this endpoint accepts came from OpenLineage, so the run's
	// source is a constant; the payload's producer is a separate field.
	const source = "openlineage"

	// The link is derived from the event with url.String(), which
	// percent-encodes, so it can be longer than the facet it came from. An
	// over-long one is dropped rather than stored: the run and task lists read
	// this column for every row of a page.
	airflowRunLogURL := openlineage.DeriveAirflowLinks(event.RawJSON).RunLogURL
	if len(airflowRunLogURL) > openlineage.MaxAirflowRunLogURLLength {
		slog.Debug("dropping an over-long Airflow run log URL",
			"runId", event.Run.RunID, "length", len(airflowRunLogURL), "limit", openlineage.MaxAirflowRunLogURLLength)
		airflowRunLogURL = ""
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
		Source:             source,
		InputCount:         int32(len(event.Inputs)),
		OutputCount:        int32(len(event.Outputs)),
		HasLineage:         derived.HasLineage,
		RawPayload:         event.RawJSON,
		AirflowRunLogURL:   airflowRunLogURL,
		Datasets:           openLineageDatasetRefs(event, derived.TaskGUID, source, derived.Integration, eventTime),
	}
}

// openLineageDatasetRefs extracts the datasets an event read or wrote, so the
// stored run carries them without a reader parsing the payload again. A dataset
// named twice in one event is one reference per direction; the schema of the
// last occurrence and the union of the column-lineage flag and fields stand for
// the run, which is what the dataset pages used to compute from the payload.
func openLineageDatasetRefs(event *openlineage.RunEvent, taskGUID, source, integration string, eventTime *time.Time) []*store.OpenLineageRunDatasetMessage {
	refs := make(map[string]*store.OpenLineageRunDatasetMessage, len(event.Inputs)+len(event.Outputs))
	order := make([]string, 0, len(event.Inputs)+len(event.Outputs))

	add := func(dataset openlineage.Dataset, direction string) {
		key := direction + "\x00" + dataset.Namespace + "\x00" + dataset.Name
		ref := refs[key]
		if ref == nil {
			ref = &store.OpenLineageRunDatasetMessage{
				TaskGUID:    taskGUID,
				Namespace:   dataset.Namespace,
				Name:        dataset.Name,
				Direction:   direction,
				EventTime:   eventTime,
				Integration: integration,
				Source:      source,
			}
			refs[key] = ref
			order = append(order, key)
		}

		if schema := dataset.Facets.Schema; schema != nil && len(schema.Fields) > 0 {
			ref.SchemaFields = encodeJSONArrayLimited(schema.Fields, maxOpenLineageSchemaFacetBytes)
		}

		if direction != store.OpenLineageDatasetDirectionOutput {
			return
		}
		ref.HasColumnLineage = ref.HasColumnLineage || hasColumnLineageFacet(dataset.Facets.ColumnLineage)
		if fields := columnLineageFieldNames(dataset.Facets.ColumnLineage); len(fields) > 0 {
			ref.ColumnLineageFields = fields
		}
	}

	for _, dataset := range event.Inputs {
		add(dataset, store.OpenLineageDatasetDirectionInput)
	}
	for _, dataset := range event.Outputs {
		add(dataset, store.OpenLineageDatasetDirectionOutput)
	}

	result := make([]*store.OpenLineageRunDatasetMessage, 0, len(order))
	for _, key := range order {
		result = append(result, refs[key])
	}
	return result
}

// columnLineageFieldNames returns the output columns a columnLineage facet
// describes, sorted so the stored JSON is the same across redeliveries.
func columnLineageFieldNames(facet *openlineage.ColumnLineageFacet) []byte {
	if facet == nil || len(facet.Fields) == 0 {
		return nil
	}
	names := make([]string, 0, len(facet.Fields))
	for name := range facet.Fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return encodeJSONArrayLimited(names, maxOpenLineageColumnLineageFieldsBytes)
}

// hasColumnLineageFacet reports whether a columnLineage facet says anything. A
// facet with neither fields nor dataset references is present but empty, and
// the badge it would raise is not true.
func hasColumnLineageFacet(facet *openlineage.ColumnLineageFacet) bool {
	return facet != nil && (len(facet.Fields) > 0 || len(facet.Dataset) > 0)
}

// encodeJSONArrayLimited marshals values into a JSON array, stopping before the
// array would exceed limit bytes. Dropping the tail keeps a stored facet bounded
// without refusing the event that carried it; the detail reads these facets, so
// an unbounded one would make that read unbounded too.
func encodeJSONArrayLimited[T any](values []T, limit int) []byte {
	encoded := make([]byte, 0, min(len(values)*16, limit))
	encoded = append(encoded, '[')
	kept := 0
	for _, value := range values {
		element, err := json.Marshal(value)
		if err != nil {
			break
		}
		// The comma that would precede this element and the closing bracket.
		if len(encoded)+len(element)+2 > limit {
			break
		}
		if kept > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, element...)
		kept++
	}
	if kept == 0 {
		return nil
	}
	return append(encoded, ']')
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
