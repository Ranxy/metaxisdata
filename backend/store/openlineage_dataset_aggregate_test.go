package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The fold one ingest transaction applies to the per-dataset aggregate. It is the
// arithmetic that keeps the list, its filter menus and the detail's summary off
// the reference table, so its shape is pinned here.
func TestOpenLineageDatasetDeltas(t *testing.T) {
	t.Parallel()

	at := func(offset time.Duration) *time.Time {
		value := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC).Add(offset)
		return &value
	}
	ref := func(namespace, name, direction, task string, eventTime *time.Time) *OpenLineageRunDatasetMessage {
		return &OpenLineageRunDatasetMessage{
			TaskGUID:    task,
			Namespace:   namespace,
			Name:        name,
			Direction:   direction,
			EventTime:   eventTime,
			Integration: "airflow",
			Source:      "openlineage",
		}
	}

	t.Run("a redelivery that changes nothing writes nothing", func(t *testing.T) {
		t.Parallel()

		held := ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))
		require.Empty(t, openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{Old: []*OpenLineageRunDatasetMessage{held}, New: []*OpenLineageRunDatasetMessage{held}}}))
	})

	t.Run("a new run's references are added", func(t *testing.T) {
		t.Parallel()

		columnLineage := ref("ns", "orders", OpenLineageDatasetDirectionOutput, "task-a", at(time.Minute))
		columnLineage.HasColumnLineage = true
		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{
			New: []*OpenLineageRunDatasetMessage{
				ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0)),
				columnLineage,
			},
		}})
		require.Len(t, deltas, 1)

		delta := deltas[0]
		require.Equal(t, "ns", delta.namespace)
		require.Equal(t, "orders", delta.name)
		require.Equal(t, 2, delta.refDelta)
		require.Equal(t, 1, delta.columnLineageRefDelta)
		require.False(t, delta.removed)
		require.NotNil(t, delta.lastSeen)
		require.True(t, delta.lastSeen.Equal(*at(time.Minute)))
		require.Equal(t, map[openLineageDatasetMemberKey]int{
			{kind: openLineageDatasetMemberInputTask, value: "task-a"}:    1,
			{kind: openLineageDatasetMemberOutputTask, value: "task-a"}:   1,
			{kind: openLineageDatasetMemberIntegration, value: "airflow"}: 2,
			{kind: openLineageDatasetMemberSource, value: "openlineage"}:  2,
		}, delta.members)
	})

	t.Run("a reference that went away is reported as a removal", func(t *testing.T) {
		t.Parallel()

		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{
			Old: []*OpenLineageRunDatasetMessage{ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))},
		}})
		require.Len(t, deltas, 1)

		delta := deltas[0]
		require.Equal(t, -1, delta.refDelta)
		require.True(t, delta.removed)
		require.Nil(t, delta.lastSeen)
		require.Equal(t, map[openLineageDatasetMemberKey]int{
			{kind: openLineageDatasetMemberInputTask, value: "task-a"}:    -1,
			{kind: openLineageDatasetMemberIntegration, value: "airflow"}: -1,
			{kind: openLineageDatasetMemberSource, value: "openlineage"}:  -1,
		}, delta.members)
	})

	t.Run("a job that moved direction keeps its count and gains the other", func(t *testing.T) {
		t.Parallel()

		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{
			Old: []*OpenLineageRunDatasetMessage{ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))},
			New: []*OpenLineageRunDatasetMessage{ref("ns", "orders", OpenLineageDatasetDirectionOutput, "task-a", at(0))},
		}})
		require.Len(t, deltas, 1)

		delta := deltas[0]
		require.Equal(t, 0, delta.refDelta, "the reference itself did not go away")
		require.True(t, delta.removed)
		require.Equal(t, map[openLineageDatasetMemberKey]int{
			{kind: openLineageDatasetMemberInputTask, value: "task-a"}:  -1,
			{kind: openLineageDatasetMemberOutputTask, value: "task-a"}: 1,
		}, delta.members)
	})

	t.Run("an absent integration and source contribute no member", func(t *testing.T) {
		t.Parallel()

		bare := ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", nil)
		bare.Integration = ""
		bare.Source = ""
		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{New: []*OpenLineageRunDatasetMessage{bare}}})
		require.Len(t, deltas, 1)
		require.Equal(t, map[openLineageDatasetMemberKey]int{
			{kind: openLineageDatasetMemberInputTask, value: "task-a"}: 1,
		}, deltas[0].members)
		require.Nil(t, deltas[0].lastSeen, "a reference without an event time cannot move last-seen")
	})

	t.Run("one batch's runs fold into one delta per dataset", func(t *testing.T) {
		t.Parallel()

		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{
			{New: []*OpenLineageRunDatasetMessage{ref("ns-b", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))}},
			{New: []*OpenLineageRunDatasetMessage{
				ref("ns-a", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0)),
				ref("ns-a", "orders", OpenLineageDatasetDirectionOutput, "task-b", at(time.Minute)),
			}},
			{New: []*OpenLineageRunDatasetMessage{ref("ns-a", "orders", OpenLineageDatasetDirectionOutput, "task-c", at(time.Hour))}},
		})

		// Datasets are ordered so their rows are locked in the same order by every
		// writer.
		require.Len(t, deltas, 2)
		require.Equal(t, "ns-a", deltas[0].namespace)
		require.Equal(t, 3, deltas[0].refDelta)
		require.Equal(t, map[openLineageDatasetMemberKey]int{
			{kind: openLineageDatasetMemberInputTask, value: "task-a"}:    1,
			{kind: openLineageDatasetMemberOutputTask, value: "task-b"}:   1,
			{kind: openLineageDatasetMemberOutputTask, value: "task-c"}:   1,
			{kind: openLineageDatasetMemberIntegration, value: "airflow"}: 3,
			{kind: openLineageDatasetMemberSource, value: "openlineage"}:  3,
		}, deltas[0].members)
		require.NotNil(t, deltas[0].lastSeen)
		require.True(t, deltas[0].lastSeen.Equal(*at(time.Hour)), "the newest of the batch's references wins")

		require.Equal(t, "ns-b", deltas[1].namespace)
		require.Equal(t, 1, deltas[1].refDelta)

		// A dataset whose references net out is not written at all.
		require.Len(t, openLineageDatasetDeltas([]openLineageRunDatasetReplacement{
			{New: []*OpenLineageRunDatasetMessage{ref("ns-a", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))}},
			{Old: []*OpenLineageRunDatasetMessage{ref("ns-a", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))}},
		}), 0)
	})

	t.Run("a replacement that only moves a reference's event time still writes", func(t *testing.T) {
		t.Parallel()

		// Every count cancels out here — the same reference with a new event time —
		// but the dataset's newest event time does not: the delta has to be applied for
		// last-seen to follow the references it belongs to, so it cannot be dropped as
		// one that writes nothing.
		deltas := openLineageDatasetDeltas([]openLineageRunDatasetReplacement{{
			Old: []*OpenLineageRunDatasetMessage{ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(0))},
			New: []*OpenLineageRunDatasetMessage{ref("ns", "orders", OpenLineageDatasetDirectionInput, "task-a", at(time.Minute))},
		}})
		require.Len(t, deltas, 1)

		delta := deltas[0]
		require.Equal(t, 0, delta.refDelta)
		require.Empty(t, delta.members)
		require.True(t, delta.removed, "the reference it replaces is gone, so the newest one has to be read back")
		require.NotNil(t, delta.lastSeen)
		require.True(t, delta.lastSeen.Equal(*at(time.Minute)))
	})
}

// The batched maintenance binds several value lists per statement, so the
// placeholders have to be numbered across the whole list rather than restarted per
// row. A row of the wrong width is a programming error and fails loudly.
func TestValuesListNumbersPlaceholdersAcrossRows(t *testing.T) {
	t.Parallel()

	list := newValuesList("text", "text")
	list.add("ns", "orders")
	list.add("ns", "daily")
	require.Equal(t, "($1::text, $2::text), ($3::text, $4::text)", list.String())
	require.Equal(t, []any{"ns", "orders", "ns", "daily"}, list.args)
	require.Equal(t, 2, list.count())

	// A column with no cast is bound as the target column types it.
	single := newValuesList("", "", "text", "", "bigint")
	single.add("ns", "orders", "integration", "airflow", 2)
	require.Equal(t, "($1, $2, $3::text, $4, $5::bigint)", single.String())

	require.Empty(t, newValuesList("text").String())
	require.Panics(t, func() { newValuesList("text", "text").add("one-value") })
}
