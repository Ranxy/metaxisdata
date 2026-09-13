package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
)

func newBatchContext(t *testing.T, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/openlineage/batch", strings.NewReader(body))
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

// The body limit alone does not bound a batch: a minimal event is a few bytes,
// so the event count is capped separately.
func TestProcessBatchEventsEnforcesEventLimit(t *testing.T) {
	t.Parallel()

	// The count check runs before parsing, so the elements only have to be
	// syntactically valid JSON.
	overLimit := "[" + strings.TrimSuffix(strings.Repeat("{},", maxOpenLineageBatchEvents+1), ",") + "]"
	ctx, rec := newBatchContext(t, overLimit)
	h := &OpenLineageHandler{}
	require.NoError(t, h.processBatchEvents(ctx, []byte(overLimit), ""))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(maxOpenLineageBatchEvents), body["limit"])

	// Exactly at the limit the request is accepted for processing; these events
	// are all unparseable, which is a 400 rather than a size rejection.
	atLimit := "[" + strings.TrimSuffix(strings.Repeat("{},", maxOpenLineageBatchEvents), ",") + "]"
	ctx, rec = newBatchContext(t, atLimit)
	require.NoError(t, h.processBatchEvents(ctx, []byte(atLimit), ""))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(maxOpenLineageBatchEvents), body["failed"])
}

// An oversized body is refused explicitly instead of being truncated into a
// confusing JSON parse error.
func TestReadBodyLimited(t *testing.T) {
	t.Parallel()

	small := strings.Repeat("a", 16)
	body, tooLarge, err := readBodyLimited(strings.NewReader(small))
	require.NoError(t, err)
	require.False(t, tooLarge)
	require.Equal(t, small, string(body))

	exact := strings.Repeat("a", maxOpenLineageBodySize)
	body, tooLarge, err = readBodyLimited(strings.NewReader(exact))
	require.NoError(t, err)
	require.False(t, tooLarge)
	require.Len(t, body, maxOpenLineageBodySize)

	_, tooLarge, err = readBodyLimited(strings.NewReader(exact + "a"))
	require.NoError(t, err)
	require.True(t, tooLarge)
}

// A non-COMPLETE event carries no run to persist; only COMPLETE events are
// written, and the batch writes them together.
func TestRunMessageForEvent(t *testing.T) {
	t.Parallel()

	h := &OpenLineageHandler{}
	event := &openlineage.RunEvent{
		EventType: "START",
		Job:       openlineage.Job{Namespace: "ns", Name: "job"},
		Run:       openlineage.Run{RunID: "run-1"},
		Inputs:    []openlineage.Dataset{{Namespace: "ns", Name: "in"}},
		Outputs:   []openlineage.Dataset{{Namespace: "ns", Name: "out"}},
	}

	run, needsPersist := h.runMessageForEvent(event)
	require.False(t, needsPersist, "a non-COMPLETE event is not persisted")
	require.NotEmpty(t, run.GUID)
	require.NotEmpty(t, run.TaskGUID)
	require.Zero(t, run.InputCount)

	event.EventType = "COMPLETE"
	event.EventTime = "2024-01-02T03:04:05Z"
	run, needsPersist = h.runMessageForEvent(event)
	require.True(t, needsPersist)
	require.Equal(t, "run-1", run.RunID)
	require.Equal(t, int32(1), run.InputCount)
	require.Equal(t, int32(1), run.OutputCount)
	require.NotNil(t, run.EventTime)
	require.Equal(t, "openlineage", run.Source)
}
