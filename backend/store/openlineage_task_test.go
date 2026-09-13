package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Ingestion folds each run into its task's counters with a delta instead of
// re-reading every run, so the delta has to reproduce what an aggregate would
// report.
func TestTaskCountDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		existed          bool
		previousHasLin   bool
		hasLineage       bool
		wantRuns         int32
		wantLineageDelta int32
	}{
		{name: "new run without lineage", wantRuns: 1},
		{name: "new run with lineage", hasLineage: true, wantRuns: 1, wantLineageDelta: 1},
		{name: "redelivered run keeps the count", existed: true},
		{name: "redelivered run gains lineage", existed: true, hasLineage: true, wantLineageDelta: 1},
		{name: "redelivered run loses lineage", existed: true, previousHasLin: true, wantLineageDelta: -1},
		{name: "redelivered run keeps lineage", existed: true, previousHasLin: true, hasLineage: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runs, lineage := taskCountDelta(test.existed, test.previousHasLin, test.hasLineage)
			require.Equal(t, test.wantRuns, runs)
			require.Equal(t, test.wantLineageDelta, lineage)
		})
	}
}

// The stored latest run mirrors the "event_time DESC NULLS LAST" ordering the
// aggregate used to apply.
func TestRunIsLatest(t *testing.T) {
	t.Parallel()

	now := time.Now()
	earlier, later := now.Add(-time.Hour), now.Add(time.Hour)

	require.True(t, runIsLatest(nil, nil), "a task without a timed run takes the first run it gets")
	require.True(t, runIsLatest(nil, &now))
	require.False(t, runIsLatest(&now, nil), "a run without an event time sorts last")
	require.True(t, runIsLatest(&now, &later))
	require.True(t, runIsLatest(&now, &now), "a redelivered run stays latest")
	require.False(t, runIsLatest(&now, &earlier))
}
