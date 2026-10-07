package v1

import (
	"context"
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

// ingestionKey is the key a batch arrives with. Its id and masked form are what a
// refusal notification names, since a rejected request has no user behind it.
func ingestionKey(scope string) *store.OpenLineageAPIKeyMessage {
	return &store.OpenLineageAPIKeyMessage{
		ID:             3,
		MaskedKey:      "mxd_ol_...ab12",
		ScopeNamespace: scope,
	}
}

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
	require.NoError(t, h.processBatchEvents(ctx, []byte(overLimit), ingestionKey("")))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(maxOpenLineageBatchEvents), body["limit"])

	// Exactly at the limit the request is accepted for processing; these events
	// are all unparseable, which is a 400 rather than a size rejection.
	atLimit := "[" + strings.TrimSuffix(strings.Repeat("{},", maxOpenLineageBatchEvents), ",") + "]"
	ctx, rec = newBatchContext(t, atLimit)
	require.NoError(t, h.processBatchEvents(ctx, []byte(atLimit), ingestionKey("")))
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
	require.NoError(t, h.processBatchEvents(ctx, []byte(oversized), ingestionKey("")))
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
	require.NoError(t, h.processBatchEvents(ctx, []byte(body), ingestionKey("")))
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

// fakeIngestionNotifier records what the handler reports, so every branch that
// ends a request without lineage can be checked for its kind and its response.
type fakeIngestionNotifier struct {
	admin      []*storepb.Notification
	namespaces []string
}

func (f *fakeIngestionNotifier) SendToWorkspaceAdmins(_ context.Context, message *storepb.Notification) error {
	f.admin = append(f.admin, message)
	return nil
}

func (f *fakeIngestionNotifier) ReportUnmatchedNamespace(_ context.Context, namespace, _ string) {
	f.namespaces = append(f.namespaces, namespace)
}

// A refused request used to be answered and forgotten: the producer saw its 4xx,
// nobody else did. Every refusal now names its cause to the administrators, and
// the kind is what tells them whether to fix a producer, a key or the server.
func TestIngestionRefusalsReachTheAdministrators(t *testing.T) {
	t.Parallel()

	overLimit := "[" + strings.TrimSuffix(strings.Repeat("{},", maxOpenLineageBatchEvents+1), ",") + "]"
	unparseable := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"ns","name":"` +
		strings.Repeat("x", openlineage.MaxJobNameLength+1) + `"}}]`
	oversizedEvent := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"ns","name":"job"},"padding":"` +
		strings.Repeat("x", openlineage.MaxEventSize) + `"}]`
	outOfScope := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"other","name":"job"}}]`

	tests := []struct {
		name       string
		body       string
		scope      string
		wantStatus int
		wantKind   storepb.OpenLineageFailureKind
	}{
		{
			name:       "an event the limits reject",
			body:       unparseable,
			wantStatus: http.StatusBadRequest,
			wantKind:   storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_INVALID_EVENT,
		},
		{
			name:       "a batch over the event count",
			body:       overLimit,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantKind:   storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED,
		},
		{
			name:       "a batch with an oversized event",
			body:       oversizedEvent,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantKind:   storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED,
		},
		{
			name:       "a key used outside its namespace",
			body:       outOfScope,
			scope:      "ns",
			wantStatus: http.StatusForbidden,
			wantKind:   storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			notifier := &fakeIngestionNotifier{}
			ctx, rec := newBatchContext(t, tc.body)
			h := &OpenLineageHandler{notifier: notifier}
			require.NoError(t, h.processBatchEvents(ctx, []byte(tc.body), ingestionKey(tc.scope)))
			require.Equal(t, tc.wantStatus, rec.Code)

			require.Len(t, notifier.admin, 1)
			message := notifier.admin[0]
			require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE, message.GetType())
			require.Equal(t, tc.wantKind, message.GetOpenlineage().GetKind())
			require.Equal(t, "mxd_ol_...ab12", message.GetOpenlineage().GetApiKey())
			require.NotEmpty(t, message.GetDedupeKey(), "a repeated failure must be suppressed by the window")
			// The suppression subject is the event's namespace when the request named
			// one, and the ingestion key when it did not parse that far.
			subject := "key/3"
			if namespace := message.GetOpenlineage().GetNamespace(); namespace != "" {
				subject = namespace
			}
			require.Contains(t, message.GetDedupeKey(), ":"+subject+":")

			wantSeverity := storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
			if tc.wantStatus >= http.StatusInternalServerError {
				wantSeverity = storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
			}
			require.Equal(t, wantSeverity, message.GetSeverity())
		})
	}
}

// The namespace a refused event named is the better suppression subject: one
// misconfigured producer is one message per window, not one per key it holds.
func TestIngestionRefusalNamesTheEventNamespace(t *testing.T) {
	t.Parallel()

	outOfScope := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"other","name":"job"}}]`
	notifier := &fakeIngestionNotifier{}
	ctx, _ := newBatchContext(t, outOfScope)
	h := &OpenLineageHandler{notifier: notifier}
	require.NoError(t, h.processBatchEvents(ctx, []byte(outOfScope), ingestionKey("ns")))

	require.Len(t, notifier.admin, 1)
	detail := notifier.admin[0].GetOpenlineage()
	require.Equal(t, "other", detail.GetNamespace())
	require.Equal(t, "job", detail.GetJob())
	require.Equal(t, "run-1", detail.GetRunId())
	require.Equal(t, int32(1), detail.GetReceivedCount())
	require.Equal(t, int32(1), detail.GetFailedCount())
	require.Contains(t, notifier.admin[0].GetDedupeKey(), "openlineage:other:")
}

// A handler built without a notifier — the unit tests, or a deployment that never
// wired one — must still answer the producer.
func TestIngestionRefusalWithoutANotifierStillAnswers(t *testing.T) {
	t.Parallel()

	notifier := &fakeIngestionNotifier{}
	body := `[{"eventType":"START","run":{"runId":"run-1"},"job":{"namespace":"other","name":"job"}}]`
	ctx, rec := newBatchContext(t, body)
	h := &OpenLineageHandler{}
	require.NoError(t, h.processBatchEvents(ctx, []byte(body), ingestionKey("ns")))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, notifier.admin)
}
