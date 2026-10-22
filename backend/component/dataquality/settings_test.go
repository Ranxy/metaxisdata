package dataquality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestDefaultSettings(t *testing.T) {
	t.Parallel()

	settings := DefaultSettings()
	require.False(t, settings.EnableLLMAnalysis)
	require.Empty(t, settings.LLMProviderProfile)
	require.True(t, settings.StoreFailureSamples, "failure samples are kept by default")
	require.Equal(t, 20, settings.FailureSampleLimit)
	require.Equal(t, time.Hour, settings.DefaultScheduleInterval)
	require.Equal(t, 10000, settings.DailyExecutionLimit)
	require.Equal(t, 50, settings.DailyColumnAnalysisLimit)
	require.Equal(t, 10000, settings.DailyLLMAnalysisLimit)
	require.Equal(t, 8, settings.LLMConcurrencyLimit)
	require.Equal(t, 50, settings.MaxColumnsPerAnalysis)
	require.False(t, settings.SendSampleValuesToLLM)
}

func TestSettingsFromProfileDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	require.Equal(t, DefaultSettings(), SettingsFromProfile(nil))
	require.Equal(t, DefaultSettings(), SettingsFromProfile(&storepb.WorkspaceProfileSetting{}))
}

func TestSettingsFromProfileResolvesExplicitValues(t *testing.T) {
	t.Parallel()

	profile := &storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{
			EnableLlmAnalysis:        true,
			LlmProviderProfile:       "llm-provider-profiles/fast",
			StoreFailureSamples:      true,
			FailureSampleLimit:       30,
			DefaultScheduleInterval:  durationpb.New(45 * time.Minute),
			DailyExecutionLimit:      500,
			DailyColumnAnalysisLimit: 80,
			DailyLlmAnalysisLimit:    20000,
			LlmConcurrencyLimit:      16,
			MaxColumnsPerAnalysis:    120,
			SendSampleValuesToLlm:    true,
		},
	}

	require.Equal(t, Settings{
		EnableLLMAnalysis:        true,
		LLMProviderProfile:       "llm-provider-profiles/fast",
		StoreFailureSamples:      true,
		FailureSampleLimit:       30,
		DefaultScheduleInterval:  45 * time.Minute,
		DailyExecutionLimit:      500,
		DailyColumnAnalysisLimit: 80,
		DailyLLMAnalysisLimit:    20000,
		LLMConcurrencyLimit:      16,
		MaxColumnsPerAnalysis:    120,
		SendSampleValuesToLLM:    true,
	}, SettingsFromProfile(profile))
}

// The two LLM limits accept zero (default) and clamp out-of-band stored values to
// the server bounds when read.
func TestSettingsFromProfileClampsTheLLMLimits(t *testing.T) {
	t.Parallel()

	profile := &storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{
			DailyLlmAnalysisLimit: MaxDailyLLMAnalysisLimit + 100,
			LlmConcurrencyLimit:   MaxLLMConcurrencyLimit + 5,
		},
	}
	settings := SettingsFromProfile(profile)
	require.Equal(t, MaxDailyLLMAnalysisLimit, settings.DailyLLMAnalysisLimit)
	require.Equal(t, MaxLLMConcurrencyLimit, settings.LLMConcurrencyLimit)

	zero := SettingsFromProfile(&storepb.WorkspaceProfileSetting{DataQuality: &storepb.DataQualitySetting{}})
	require.Equal(t, DefaultDailyLLMAnalysisLimit, zero.DailyLLMAnalysisLimit)
	require.Equal(t, DefaultLLMConcurrencyLimit, zero.LLMConcurrencyLimit)
}

// The settings page writes every boolean explicitly, so a present data_quality
// message means the booleans are authoritative: a false store_failure_samples
// must stay false even though the default is true.
func TestSettingsFromProfileKeepsExplicitlyDisabledBooleans(t *testing.T) {
	t.Parallel()

	profile := &storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{StoreFailureSamples: false},
	}
	settings := SettingsFromProfile(profile)
	require.False(t, settings.StoreFailureSamples)
	require.False(t, settings.EnableLLMAnalysis)
	require.False(t, settings.SendSampleValuesToLLM)
	// Numeric zeros still mean the server default.
	require.Equal(t, 20, settings.FailureSampleLimit)
	require.Equal(t, time.Hour, settings.DefaultScheduleInterval)
}

func TestSettingsFromProfileClampsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataQuality    *storepb.DataQualitySetting
		wantSample     int
		wantInterval   time.Duration
		wantDailyExec  int
		wantDailyCols  int
		wantMaxColumns int
	}{
		{
			name:        "negative sample limit reads as default",
			dataQuality: &storepb.DataQualitySetting{FailureSampleLimit: -1},
			wantSample:  DefaultFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "sample limit above the hard cap is capped",
			dataQuality: &storepb.DataQualitySetting{FailureSampleLimit: 1000},
			wantSample:  MaxFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "sample limit at the cap is kept",
			dataQuality: &storepb.DataQualitySetting{FailureSampleLimit: MaxFailureSampleLimit},
			wantSample:  MaxFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "interval below the minimum is raised",
			dataQuality: &storepb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(time.Minute)},
			wantSample:  DefaultFailureSampleLimit, wantInterval: MinScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "interval above the maximum is lowered",
			dataQuality: &storepb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(30 * 24 * time.Hour)},
			wantSample:  DefaultFailureSampleLimit, wantInterval: MaxScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "negative interval reads as default",
			dataQuality: &storepb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(-time.Hour)},
			wantSample:  DefaultFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name:        "zero interval inside a present message reads as default",
			dataQuality: &storepb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(0)},
			wantSample:  DefaultFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name: "negative quotas read as defaults",
			dataQuality: &storepb.DataQualitySetting{
				DailyExecutionLimit:      -10,
				DailyColumnAnalysisLimit: -1,
				MaxColumnsPerAnalysis:    -1,
			},
			wantSample: DefaultFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: DefaultDailyExecutionLimit, wantDailyCols: DefaultDailyColumnAnalysisLimit,
			wantMaxColumns: DefaultMaxColumnsPerAnalysis,
		},
		{
			name: "positive quotas are kept",
			dataQuality: &storepb.DataQualitySetting{
				DailyExecutionLimit:      3,
				DailyColumnAnalysisLimit: 4,
				MaxColumnsPerAnalysis:    5,
			},
			wantSample: DefaultFailureSampleLimit, wantInterval: DefaultScheduleInterval,
			wantDailyExec: 3, wantDailyCols: 4, wantMaxColumns: 5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := SettingsFromProfile(&storepb.WorkspaceProfileSetting{DataQuality: test.dataQuality})
			require.Equal(t, test.wantSample, settings.FailureSampleLimit)
			require.Equal(t, test.wantInterval, settings.DefaultScheduleInterval)
			require.Equal(t, test.wantDailyExec, settings.DailyExecutionLimit)
			require.Equal(t, test.wantDailyCols, settings.DailyColumnAnalysisLimit)
			require.Equal(t, test.wantMaxColumns, settings.MaxColumnsPerAnalysis)
		})
	}
}

func TestSettingsFromProfileKeepsProviderProfile(t *testing.T) {
	t.Parallel()

	profile := &storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{LlmProviderProfile: " llm-provider-profiles/fast "},
	}
	require.Equal(t, "llm-provider-profiles/fast", SettingsFromProfile(profile).LLMProviderProfile)
}

func TestSettingsFromValue(t *testing.T) {
	t.Parallel()

	for _, empty := range []string{"", "   \n\t"} {
		settings, err := settingsFromValue(empty)
		require.NoError(t, err)
		require.Equal(t, DefaultSettings(), settings)
	}

	settings, err := settingsFromValue(`{
		"dataQuality": {
			"enableLlmAnalysis": true,
			"llmProviderProfile": "llm-provider-profiles/fast",
			"storeFailureSamples": false,
			"failureSampleLimit": 250,
			"defaultScheduleInterval": "2700s",
			"dailyExecutionLimit": -5,
			"dailyColumnAnalysisLimit": 80,
			"maxColumnsPerAnalysis": 120,
			"sendSampleValuesToLlm": true
		}
	}`)
	require.NoError(t, err)
	require.True(t, settings.EnableLLMAnalysis)
	require.Equal(t, "llm-provider-profiles/fast", settings.LLMProviderProfile)
	require.False(t, settings.StoreFailureSamples)
	require.Equal(t, MaxFailureSampleLimit, settings.FailureSampleLimit)
	require.Equal(t, 45*time.Minute, settings.DefaultScheduleInterval)
	require.Equal(t, DefaultDailyExecutionLimit, settings.DailyExecutionLimit)
	require.Equal(t, 80, settings.DailyColumnAnalysisLimit)
	require.Equal(t, 120, settings.MaxColumnsPerAnalysis)
	require.True(t, settings.SendSampleValuesToLLM)

	_, err = settingsFromValue(`{"dataQuality":`)
	require.Error(t, err)
}

func TestLoadSettingsRejectsNilStore(t *testing.T) {
	t.Parallel()

	_, err := LoadSettings(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}
