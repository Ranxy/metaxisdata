package quality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestQualityEnumNameOmitsTheUnspecifiedValue(t *testing.T) {
	t.Parallel()

	require.Empty(t, qualityEnumName(v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED))
	require.Equal(t, "QUALITY_STATUS_FAIL", qualityEnumName(v1pb.QualityStatus_QUALITY_STATUS_FAIL))
}

func TestQualityStatusJudgedKeepsMeasurementsApartFromOutcomes(t *testing.T) {
	t.Parallel()

	require.True(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_PASS))
	require.True(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_WARN))
	require.True(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_FAIL))
	require.False(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_ERROR))
	require.False(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_SKIPPED))
	require.False(t, qualityStatusJudged(v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
}

func TestQualityTargetAllPausedNeedsChecksThatAllExistButArePaused(t *testing.T) {
	t.Parallel()

	require.True(t, qualityTargetAllPaused(&v1pb.QualityTargetSummary{RuleCount: 2, EnabledRuleCount: 0}))
	require.False(t, qualityTargetAllPaused(&v1pb.QualityTargetSummary{RuleCount: 2, EnabledRuleCount: 1}))
	require.False(t, qualityTargetAllPaused(&v1pb.QualityTargetSummary{}), "a target with no checks at all is not all-paused")
}

func TestQualitySignalTimeRendersAUTCInstant(t *testing.T) {
	t.Parallel()

	require.Empty(t, qualitySignalTime(nil))
	require.Empty(t, qualitySignalTime(timestamppb.New(time.Unix(0, 0))))
	require.Equal(t, "2026-01-02T03:04:05Z",
		qualitySignalTime(timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))))
}
