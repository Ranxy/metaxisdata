package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
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

// Every run state is written into the run's one row, because a row is the run's
// latest known state. A job that is still running or that failed is therefore
// visible instead of leaving no trace at all.
func TestRunMessageForEvent(t *testing.T) {
	t.Parallel()

	h := &OpenLineageHandler{}
	event := &openlineage.RunEvent{
		EventType: "START",
		EventTime: "2024-01-02T03:04:05Z",
		Job:       openlineage.Job{Namespace: "ns", Name: "job"},
		Run:       openlineage.Run{RunID: "run-1"},
		Inputs:    []openlineage.Dataset{{Namespace: "ns", Name: "in"}},
		Outputs:   []openlineage.Dataset{{Namespace: "ns", Name: "out"}},
	}

	run := h.runMessageForEvent(event)
	require.Equal(t, "START", run.EventType)
	require.Equal(t, "run-1", run.RunID)
	require.NotEmpty(t, run.GUID)
	require.NotEmpty(t, run.TaskGUID)
	require.Equal(t, int32(1), run.InputCount)
	require.Equal(t, int32(1), run.OutputCount)
	require.NotNil(t, run.EventTime)
	require.Equal(t, "openlineage", run.Source)

	// The states of one run share an identity, which is what lets them share a row.
	startGUID := run.GUID
	event.EventType = "COMPLETE"
	run = h.runMessageForEvent(event)
	require.Equal(t, "COMPLETE", run.EventType)
	require.Equal(t, startGUID, run.GUID)
}

// Producers sometimes omit the timezone offset. Dropping such a timestamp stores
// NULL, which sorts last and is exempt from retention, so it is assumed UTC.
func TestParseEventTime(t *testing.T) {
	t.Parallel()

	withOffset, err := parseEventTime("2024-01-02T03:04:05+08:00")
	require.NoError(t, err)
	require.Equal(t, "2024-01-01T19:04:05Z", withOffset.Format(time.RFC3339))

	withoutOffset, err := parseEventTime("2024-01-02T03:04:05")
	require.NoError(t, err)
	require.Equal(t, "2024-01-02T03:04:05Z", withoutOffset.Format(time.RFC3339))

	fractional, err := parseEventTime("2024-01-02T03:04:05.123")
	require.NoError(t, err)
	require.Equal(t, "2024-01-02T03:04:05.123Z", fractional.Format(time.RFC3339Nano))

	_, err = parseEventTime("not-a-time")
	require.Error(t, err)
}

// Ingestion audit rows reuse the Connect audit mapping, so the HTTP status has
// to be turned back into the error shape those helpers understand.
func TestAuditErrorForHTTPStatus(t *testing.T) {
	t.Parallel()

	require.NoError(t, audit.ErrorForHTTPStatus(http.StatusOK))
	require.NoError(t, audit.ErrorForHTTPStatus(http.StatusCreated))

	unauthorized := audit.ErrorForHTTPStatus(http.StatusUnauthorized)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(unauthorized))
	require.Equal(t, storepb.AuditLogSeverity_WARNING, audit.MapSeverity(unauthorized))

	tooLarge := audit.ErrorForHTTPStatus(http.StatusRequestEntityTooLarge)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(tooLarge))

	serverErr := audit.ErrorForHTTPStatus(http.StatusInternalServerError)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(serverErr))
	require.Equal(t, storepb.AuditLogSeverity_ERROR, audit.MapSeverity(serverErr))
}

// A batch carrying one event over the per-event size limit is refused as a
// whole, like an over-limit body: the size is a property of the transport, and
// the batch is written atomically anyway. The handler returns before touching
// the store, which the nil store here proves.
func TestProcessBatchEventsRejectsAnOversizedEvent(t *testing.T) {
	t.Parallel()

	oversized := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"ns","name":"job"},"padding":"` +
		strings.Repeat("x", openlineage.MaxEventSize) + `"}]`
	ctx, rec := newBatchContext(t, oversized)
	h := &OpenLineageHandler{}
	require.NoError(t, h.processBatchEvents(ctx, []byte(oversized), ""))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(0), body["index"])
	require.Equal(t, float64(openlineage.MaxEventSize), body["limit"])
}

// An event whose identity exceeds a field limit is an invalid event, not an
// over-limit request: a batch skips it the same way it skips an unparseable one.
func TestProcessBatchEventsSkipsEventsOverTheFieldLimits(t *testing.T) {
	t.Parallel()

	overlong := strings.Repeat("x", openlineage.MaxJobNameLength+1)
	body := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"ns","name":"` + overlong + `"}},` +
		`{"eventType":"START","run":{"runId":"run-2"},"job":{"namespace":"ns","name":"` + overlong + `"}}]`
	ctx, rec := newBatchContext(t, body)
	h := &OpenLineageHandler{}
	require.NoError(t, h.processBatchEvents(ctx, []byte(body), ""))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, float64(2), response["failed"])
}

// The datasets an event names are extracted once, at ingestion, so the dataset
// pages aggregate stored references instead of the payload.
func TestOpenLineageDatasetRefs(t *testing.T) {
	t.Parallel()

	event := &openlineage.RunEvent{
		Inputs: []openlineage.Dataset{{
			Namespace: "ns",
			Name:      "public.orders",
			Facets: openlineage.DatasetFacets{
				Schema: &openlineage.SchemaFacet{Fields: []openlineage.SchemaField{{Name: "id", Type: "INT"}}},
				// An input's column-lineage facet says nothing about what the run
				// wrote, so it does not raise the flag.
				ColumnLineage: &openlineage.ColumnLineageFacet{Fields: map[string]openlineage.ColumnLineageField{"id": {}}},
			},
		}},
		Outputs: []openlineage.Dataset{
			{
				Namespace: "ns",
				Name:      "public.daily_orders",
				Facets: openlineage.DatasetFacets{
					Schema:        &openlineage.SchemaFacet{Fields: []openlineage.SchemaField{{Name: "total", Type: "NUMERIC"}}},
					ColumnLineage: &openlineage.ColumnLineageFacet{Fields: map[string]openlineage.ColumnLineageField{"total": {}, "id": {}}},
				},
			},
			// The same output again, without facets: one reference per direction,
			// and the facets the first occurrence stated survive.
			{Namespace: "ns", Name: "public.daily_orders"},
		},
	}
	eventTime := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	refs := openLineageDatasetRefs(event, "openlineage:task:TASK:ns:job", "openlineage", "airflow", &eventTime)
	require.Len(t, refs, 2)

	input := refs[0]
	require.Equal(t, store.OpenLineageDatasetDirectionInput, input.Direction)
	require.Equal(t, "public.orders", input.Name)
	require.False(t, input.HasColumnLineage)
	require.Empty(t, input.ColumnLineageFields)
	require.JSONEq(t, `[{"name":"id","type":"INT"}]`, string(input.SchemaFields))
	require.Equal(t, "openlineage:task:TASK:ns:job", input.TaskGUID)
	require.Equal(t, "airflow", input.Integration)
	require.Equal(t, "openlineage", input.Source)
	require.NotNil(t, input.EventTime)

	output := refs[1]
	require.Equal(t, store.OpenLineageDatasetDirectionOutput, output.Direction)
	require.Equal(t, "public.daily_orders", output.Name)
	require.True(t, output.HasColumnLineage)
	require.JSONEq(t, `["id","total"]`, string(output.ColumnLineageFields))
	require.JSONEq(t, `[{"name":"total","type":"NUMERIC"}]`, string(output.SchemaFields))
}

// The Airflow link is materialized on the run row, so the run list and the task
// list can render it without the payload it came from.
func TestRunMessageForEventMaterializesTheAirflowLink(t *testing.T) {
	t.Parallel()

	event, err := openlineage.ParseRunEvent([]byte(`{
		"eventType":"COMPLETE",
		"run":{"runId":"run-1","facets":{"airflow":{"taskInstance":{"log_url":"https://airflow.example.com/dags/x/runs/1"}}}},
		"job":{"namespace":"ns","name":"job"},
		"outputs":[{"namespace":"ns","name":"out"}]
	}`))
	require.NoError(t, err)

	h := &OpenLineageHandler{}
	run := h.runMessageForEvent(event)
	require.Equal(t, "https://airflow.example.com/dags/x/runs/1", run.AirflowRunLogURL)
	require.Len(t, run.Datasets, 1)
	require.Equal(t, "out", run.Datasets[0].Name)
}

// The Airflow link is stored so the run and task lists can render it without the
// payload; an over-long one is dropped rather than stored and read back for
// every row of a page. url.String() percent-encodes, so the stored link can be
// longer than the facet it came from.
func TestRunMessageForEventDropsAnOverlongAirflowLink(t *testing.T) {
	t.Parallel()

	overlong, err := openlineage.ParseRunEvent([]byte(`{
		"eventType":"COMPLETE",
		"run":{"runId":"run-1","facets":{"airflow":{"taskInstance":{"log_url":"https://airflow.example.com/dags/x/` +
		strings.Repeat("y", openlineage.MaxAirflowRunLogURLLength) + `"}}}},
		"job":{"namespace":"ns","name":"job"}
	}`))
	require.NoError(t, err)

	h := &OpenLineageHandler{}
	require.Empty(t, h.runMessageForEvent(overlong).AirflowRunLogURL)

	withinCap, err := openlineage.ParseRunEvent([]byte(`{
		"eventType":"COMPLETE",
		"run":{"runId":"run-2","facets":{"airflow":{"taskInstance":{"log_url":"https://airflow.example.com/dags/x/runs/1"}}}},
		"job":{"namespace":"ns","name":"job"}
	}`))
	require.NoError(t, err)
	require.Equal(t, "https://airflow.example.com/dags/x/runs/1", h.runMessageForEvent(withinCap).AirflowRunLogURL)
}

// The schema and column-lineage facets are stored so the dataset detail does not
// read the payload; the stored copies are capped so one dataset's facets cannot
// make that read expand an unbounded JSONB. The entries that fit are kept.
func TestOpenLineageDatasetRefsCapsTheStoredFacets(t *testing.T) {
	t.Parallel()

	const fieldCount = 4000
	fields := make([]openlineage.SchemaField, 0, fieldCount)
	lineageFields := make(map[string]openlineage.ColumnLineageField, fieldCount)
	for i := range fieldCount {
		fields = append(fields, openlineage.SchemaField{
			Name:        fmt.Sprintf("field_%04d", i),
			Type:        "VARCHAR(255)",
			Description: strings.Repeat("d", 40),
		})
		lineageFields[fmt.Sprintf("column_%04d", i)] = openlineage.ColumnLineageField{}
	}

	event := &openlineage.RunEvent{
		Outputs: []openlineage.Dataset{{
			Namespace: "ns",
			Name:      "out",
			Facets: openlineage.DatasetFacets{
				Schema:        &openlineage.SchemaFacet{Fields: fields},
				ColumnLineage: &openlineage.ColumnLineageFacet{Fields: lineageFields},
			},
		}},
	}

	refs := openLineageDatasetRefs(event, "openlineage:task:TASK:ns:job", "openlineage", "airflow", nil)
	require.Len(t, refs, 1)
	ref := refs[0]

	require.LessOrEqual(t, len(ref.SchemaFields), maxOpenLineageSchemaFacetBytes)
	var storedFields []openlineage.SchemaField
	require.NoError(t, json.Unmarshal(ref.SchemaFields, &storedFields))
	require.NotEmpty(t, storedFields)
	require.Less(t, len(storedFields), fieldCount, "the fields that do not fit are dropped")
	require.Equal(t, "field_0000", storedFields[0].Name, "the fields that fit are the leading ones")

	require.LessOrEqual(t, len(ref.ColumnLineageFields), maxOpenLineageColumnLineageFieldsBytes)
	var storedNames []string
	require.NoError(t, json.Unmarshal(ref.ColumnLineageFields, &storedNames))
	require.NotEmpty(t, storedNames)
	require.Less(t, len(storedNames), fieldCount)
	require.True(t, slices.IsSorted(storedNames), "the stored names are sorted so a redelivery stores the same JSON")
}
