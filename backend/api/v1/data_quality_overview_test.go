package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestQualityOverviewTrendDays pins the clamp: a missing window is the default,
// a larger one is capped, and a negative one cannot shrink the chart to nothing.
func TestQualityOverviewTrendDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, 7, qualityOverviewTrendDays(0))
	require.Equal(t, 7, qualityOverviewTrendDays(-3))
	require.Equal(t, 14, qualityOverviewTrendDays(14))
	require.Equal(t, 30, qualityOverviewTrendDays(31))
}

// TestFillQualityTrend pins that a quiet day is a zero point rather than a gap,
// and that the points are ordered oldest first.
func TestFillQualityTrend(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC)
	trend := []store.QualityTrendDay{
		{Date: "2026-10-18", Counts: &storepb.QualityStatusCounts{Total: 3, Pass: 2, Fail: 1}},
		{Date: "2026-10-20", Counts: &storepb.QualityStatusCounts{Total: 1, Error: 1}},
	}
	points := fillQualityTrend(start, 3, trend)
	require.Len(t, points, 3)
	require.Equal(t, "2026-10-18", points[0].GetDate())
	require.Equal(t, int32(2), points[0].GetPass())
	require.Equal(t, "2026-10-19", points[1].GetDate())
	require.Equal(t, int32(0), points[1].GetTotal())
	require.Equal(t, "2026-10-20", points[2].GetDate())
	require.Equal(t, int32(1), points[2].GetError())
	require.Equal(t, int32(1), points[2].GetTotal())
}
