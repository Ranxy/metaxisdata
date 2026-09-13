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

// A list request without a size is capped instead of reading the whole table.
func TestOpenLineagePageClause(t *testing.T) {
	t.Parallel()

	five := 5
	big := 20000
	zero := 0
	negative := -3
	offset := 40

	clause, args := openLineagePageClause(nil, nil, 0)
	require.Equal(t, " LIMIT $1", clause)
	require.Equal(t, []any{5000}, args)

	_, args = openLineagePageClause(&five, nil, 0)
	require.Equal(t, []any{5}, args)

	_, args = openLineagePageClause(&big, nil, 0)
	require.Equal(t, []any{20000}, args, "a caller may ask for more than the default")

	_, args = openLineagePageClause(&zero, nil, 0)
	require.Equal(t, []any{0}, args, "an explicit empty page stays empty")

	// PostgreSQL rejects a negative LIMIT, so it is clamped instead.
	_, args = openLineagePageClause(&negative, &offset, 0)
	require.Equal(t, []any{0, 40}, args)

	clause, args = openLineagePageClause(nil, &offset, 2)
	require.Equal(t, " LIMIT $3 OFFSET $4", clause)
	require.Equal(t, []any{5000, 40}, args, "placeholders continue after the caller's arguments")
}

// A batch is persisted in the caller's order even though its tasks are locked in
// a canonical order: the handler matches each returned run with the event that
// produced it, so a reordered result would write lineage under the wrong run.
func TestTaskLockOrderLocksCanonicallyAndKeepsRunOrder(t *testing.T) {
	t.Parallel()

	runs := []*OpenLineageRunMessage{
		{TaskGUID: "task-b", RunID: "b1"},
		{TaskGUID: "task-a", RunID: "a1"},
		{TaskGUID: "task-b", RunID: "b2"},
	}

	require.Equal(t, []int{1, 0, 2}, taskLockOrder(runs), "tasks ascending, runs of one task in input order")
	require.Equal(t, "b1", runs[taskLockOrder(runs)[1]].RunID)
	require.Empty(t, taskLockOrder(nil))
}
