package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The per-target trend is only usable if it is scoped: without the exact target
// GUID it would report another target's executions, and without the COMPLETED
// state it would count a merely queued execution as a result. It must also bucket
// days in UTC, the same way the workspace trend does, or a report's day boundary
// disagrees with the overview's.
func TestQualityTargetTrendQueryIsScoped(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityTargetTrendQuery, "target_guid = $1")
	require.Contains(t, qualityTargetTrendQuery, "state = $2")
	require.Contains(t, qualityTargetTrendQuery, "started_at >= $3")
	require.Contains(t, qualityTargetTrendQuery, "AT TIME ZONE 'UTC'")
	require.Contains(t, qualityTargetTrendQuery, "GROUP BY day, status")
	require.Contains(t, qualityTargetTrendQuery, "ORDER BY day")
}

// A missing target or window is a caller bug; the query must not run and turn it
// into an empty chart that reads as "nothing failed".
func TestQualityTargetExecutionTrendValidatesInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := (&Store{}).QualityTargetExecutionTrend(ctx, "", time.Now())
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = (&Store{}).QualityTargetExecutionTrend(ctx, "instance-1;shop;;orders", time.Time{})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The accumulator maps every six-state value onto its own tally, keeps the days
// in first-seen order, ignores a row without a day, and leaves an unknown status
// out of the named tallies (its count still belongs to the total).
func TestQualityTrendAccumulator(t *testing.T) {
	t.Parallel()

	accumulator := newQualityTrendAccumulator()
	accumulator.add("2026-10-19", storepb.QualityStatus_QUALITY_STATUS_PASS, 2)
	accumulator.add("2026-10-19", storepb.QualityStatus_QUALITY_STATUS_FAIL, 1)
	accumulator.add("2026-10-18", storepb.QualityStatus_QUALITY_STATUS_WARN, 3)
	accumulator.add("2026-10-18", storepb.QualityStatus_QUALITY_STATUS_ERROR, 1)
	accumulator.add("2026-10-18", storepb.QualityStatus_QUALITY_STATUS_SKIPPED, 1)
	accumulator.add("2026-10-18", storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 1)
	accumulator.add("2026-10-17", storepb.QualityStatus(99), 4)
	accumulator.add("", storepb.QualityStatus_QUALITY_STATUS_PASS, 100)

	trend := accumulator.trend()
	require.Len(t, trend, 3)
	require.Equal(t, "2026-10-19", trend[0].Date)
	require.Equal(t, int32(3), trend[0].Counts.GetTotal())
	require.Equal(t, int32(2), trend[0].Counts.GetPass())
	require.Equal(t, int32(1), trend[0].Counts.GetFail())

	require.Equal(t, "2026-10-18", trend[1].Date)
	require.Equal(t, int32(6), trend[1].Counts.GetTotal())
	require.Equal(t, int32(3), trend[1].Counts.GetWarn())
	require.Equal(t, int32(1), trend[1].Counts.GetError())
	require.Equal(t, int32(1), trend[1].Counts.GetSkipped())
	require.Equal(t, int32(1), trend[1].Counts.GetNotEvaluated())

	// An unknown status counts toward the total but has no named tally.
	require.Equal(t, "2026-10-17", trend[2].Date)
	require.Equal(t, int32(4), trend[2].Counts.GetTotal())
	require.Zero(t, trend[2].Counts.GetPass()+trend[2].Counts.GetWarn()+
		trend[2].Counts.GetFail()+trend[2].Counts.GetError()+
		trend[2].Counts.GetSkipped()+trend[2].Counts.GetNotEvaluated())

	// An accumulator that saw no row returns an empty, non-nil trend so the
	// caller's gap filling starts from a clean slice.
	require.Empty(t, newQualityTrendAccumulator().trend())
}
