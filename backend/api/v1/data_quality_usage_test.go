package v1

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The usage view is assembled from the resolved settings and the live counters:
// effective limits (defaults filled, out-of-band clamped) plus used/in-flight.
func TestQualityLLMUsageResponse(t *testing.T) {
	t.Parallel()

	resetsAt := time.Date(2026, time.October, 23, 0, 0, 0, 0, time.UTC)
	settings := dataquality.Settings{
		DailyLLMAnalysisLimit:    42,
		DailyColumnAnalysisLimit: 7,
		LLMConcurrencyLimit:      3,
	}
	usage := qualityLLMUsageResponse(settings, 5, 2, 1, resetsAt)
	require.Equal(t, int32(5), usage.GetDailyAnalysisUsed())
	require.Equal(t, int32(42), usage.GetDailyAnalysisLimit())
	require.Equal(t, int32(2), usage.GetDailyColumnAnalysisUsed())
	require.Equal(t, int32(7), usage.GetDailyColumnAnalysisLimit())
	require.Equal(t, int32(1), usage.GetInFlight())
	require.Equal(t, int32(3), usage.GetConcurrencyLimit())
	require.Equal(t, timestamppb.New(resetsAt), usage.GetResetsAt())

	// A workspace that never saved the limits reports the server defaults, not
	// zeros.
	defaults := qualityLLMUsageResponse(dataquality.DefaultSettings(), 0, 0, 0, resetsAt)
	require.Equal(t, int32(dataquality.DefaultDailyLLMAnalysisLimit), defaults.GetDailyAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultDailyColumnAnalysisLimit), defaults.GetDailyColumnAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultLLMConcurrencyLimit), defaults.GetConcurrencyLimit())

	// An out-of-band stored value is clamped in the view, which is what the quota
	// enforces.
	clamped := qualityLLMUsageResponse(dataquality.Settings{
		DailyLLMAnalysisLimit: dataquality.MaxDailyLLMAnalysisLimit + 1,
		LLMConcurrencyLimit:   dataquality.MaxLLMConcurrencyLimit + 1,
	}, 0, 0, 0, resetsAt)
	require.Equal(t, int32(dataquality.MaxDailyLLMAnalysisLimit), clamped.GetDailyAnalysisLimit())
	require.Equal(t, int32(dataquality.MaxLLMConcurrencyLimit), clamped.GetConcurrencyLimit())
}

// The settings API exposes the stored LLM limits, resolving zeros to the server
// defaults.
func TestResolveDataQualitySettingExposesTheLLMLimits(t *testing.T) {
	t.Parallel()

	stored := resolveDataQualitySetting(&storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{DailyLlmAnalysisLimit: 9000, LlmConcurrencyLimit: 5},
	})
	require.Equal(t, int32(9000), stored.GetDailyLlmAnalysisLimit())
	require.Equal(t, int32(5), stored.GetLlmConcurrencyLimit())

	defaults := resolveDataQualitySetting(&storepb.WorkspaceProfileSetting{})
	require.Equal(t, int32(dataquality.DefaultDailyLLMAnalysisLimit), defaults.GetDailyLlmAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultLLMConcurrencyLimit), defaults.GetLlmConcurrencyLimit())
}

// The handler validates its envelope before touching the store.
func TestGetQualityLLMUsageRequiresARequest(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	_, err := service.GetQualityLLMUsage(t.Context(), nil)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
