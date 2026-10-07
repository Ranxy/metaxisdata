package openlineage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustParseRunEvent(t *testing.T, payload string) *RunEvent {
	t.Helper()
	event, err := ParseRunEvent([]byte(payload))
	require.NoError(t, err)
	return event
}

// A reasonable event passes, including the facets the dataset pages read.
func TestValidateEventLimitsAcceptsAReasonableEvent(t *testing.T) {
	t.Parallel()

	event := mustParseRunEvent(t, `{
		"eventType":"COMPLETE",
		"eventTime":"2024-01-02T03:04:05Z",
		"run":{"runId":"run-1"},
		"job":{"namespace":"postgres://warehouse:5432/analytics","name":"dag.task","facets":{"jobType":{"jobType":"TASK"}}},
		"inputs":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders","facets":{"schema":{"fields":[{"name":"id","type":"INT"}]}}}],
		"outputs":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.daily"}]
	}`)
	require.NoError(t, ValidateEventLimits(event))
}

// An event over the per-event size limit reports the sentinel the handler turns
// into a 413, rather than the 400 a malformed event earns.
func TestValidateEventLimitsRejectsAnOversizedEvent(t *testing.T) {
	t.Parallel()

	event := &RunEvent{RawJSON: []byte(strings.Repeat("a", MaxEventSize+1))}
	err := ValidateEventLimits(event)
	require.ErrorIs(t, err, ErrEventTooLarge)
	require.Contains(t, err.Error(), "limit")

	atLimit := &RunEvent{RawJSON: []byte(strings.Repeat("a", MaxEventSize))}
	// The remaining fields are empty, which is a valid event identity; the point
	// is that the size check itself does not fire.
	require.NoError(t, ValidateEventLimits(atLimit))
}

// A value too long for the identity's unique btree index used to be stored and
// then fail the insert forever. It is rejected as an invalid event instead.
func TestValidateEventLimitsRejectsOverlongFields(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 4096)
	tests := []struct {
		name   string
		mutate func(event *RunEvent)
	}{
		{name: "job namespace", mutate: func(event *RunEvent) { event.Job.Namespace = long }},
		{name: "job name", mutate: func(event *RunEvent) { event.Job.Name = long }},
		{name: "run id", mutate: func(event *RunEvent) { event.Run.RunID = long }},
		{name: "dataset namespace", mutate: func(event *RunEvent) { event.Inputs[0].Namespace = long }},
		{name: "dataset name", mutate: func(event *RunEvent) { event.Outputs[0].Name = long }},
		{
			name: "job type",
			mutate: func(event *RunEvent) {
				event.Job.Facets = map[string]json.RawMessage{"jobType": json.RawMessage(`{"jobType":"` + long + `"}`)}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event := mustParseRunEvent(t, `{
				"eventType":"COMPLETE",
				"run":{"runId":"run-1"},
				"job":{"namespace":"ns","name":"job"},
				"inputs":[{"namespace":"ns","name":"in"}],
				"outputs":[{"namespace":"ns","name":"out"}]
			}`)
			test.mutate(event)

			err := ValidateEventLimits(event)
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrEventTooLarge)
			require.Contains(t, err.Error(), "limit is")
		})
	}
}

// The per-field caps can still let every field be at its limit and every
// character need escaping; the GUID the run is indexed under has to stay under
// PostgreSQL's index item size.
func TestValidateEventLimitsBoundsTheEscapedGUID(t *testing.T) {
	t.Parallel()

	event := &RunEvent{
		EventType: "COMPLETE",
		Run:       Run{RunID: strings.Repeat("?", MaxRunIDLength)},
		Job:       Job{Namespace: strings.Repeat("?", MaxJobNamespaceLength), Name: strings.Repeat("?", MaxJobNameLength)},
	}
	err := ValidateEventLimits(event)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GUID")
}

// One event may not declare an unbounded number of datasets: each one becomes a
// stored reference.
func TestValidateEventLimitsRejectsTooManyDatasets(t *testing.T) {
	t.Parallel()

	event := &RunEvent{
		EventType: "COMPLETE",
		Run:       Run{RunID: "run-1"},
		Job:       Job{Namespace: "ns", Name: "job"},
	}
	for range MaxEventDatasets + 1 {
		event.Inputs = append(event.Inputs, Dataset{Namespace: "ns", Name: "in"})
	}
	err := ValidateEventLimits(event)
	require.Error(t, err)
	require.Contains(t, err.Error(), "datasets")
}
