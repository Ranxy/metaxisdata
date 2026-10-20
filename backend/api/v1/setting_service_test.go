package v1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestUpdateDebugConfigDrivesRuntimeDebugAndLogLevel(t *testing.T) {
	t.Cleanup(func() { log.LogLevel.Set(slog.LevelInfo) })

	profile := &config.Profile{}
	service := NewSettingService(nil, profile)

	enabled, err := service.UpdateDebugConfig(context.Background(), connect.NewRequest(&v1pb.UpdateDebugConfigRequest{Enabled: true}))
	if err != nil {
		t.Fatalf("UpdateDebugConfig(enabled=true) returned error: %v", err)
	}
	if !enabled.Msg.GetEnabled() {
		t.Error("UpdateDebugConfig(enabled=true) response = false, want true")
	}
	if !profile.RuntimeDebug.Load() {
		t.Error("RuntimeDebug = false after enabling, want true")
	}
	if got := log.LogLevel.Level(); got != slog.LevelDebug {
		t.Errorf("log level = %v after enabling, want %v", got, slog.LevelDebug)
	}

	disabled, err := service.UpdateDebugConfig(context.Background(), connect.NewRequest(&v1pb.UpdateDebugConfigRequest{Enabled: false}))
	if err != nil {
		t.Fatalf("UpdateDebugConfig(enabled=false) returned error: %v", err)
	}
	if disabled.Msg.GetEnabled() {
		t.Error("UpdateDebugConfig(enabled=false) response = true, want false")
	}
	if profile.RuntimeDebug.Load() {
		t.Error("RuntimeDebug = true after disabling, want false")
	}
	if got := log.LogLevel.Level(); got != slog.LevelInfo {
		t.Errorf("log level = %v after disabling, want %v", got, slog.LevelInfo)
	}
}

func TestGetDebugConfigReportsRuntimeDebug(t *testing.T) {
	profile := &config.Profile{}
	service := NewSettingService(nil, profile)

	off, err := service.GetDebugConfig(context.Background(), connect.NewRequest(&v1pb.GetDebugConfigRequest{}))
	if err != nil {
		t.Fatalf("GetDebugConfig returned error: %v", err)
	}
	if off.Msg.GetEnabled() {
		t.Error("GetDebugConfig = true with RuntimeDebug off, want false")
	}

	profile.RuntimeDebug.Store(true)
	on, err := service.GetDebugConfig(context.Background(), connect.NewRequest(&v1pb.GetDebugConfigRequest{}))
	if err != nil {
		t.Fatalf("GetDebugConfig returned error: %v", err)
	}
	if !on.Msg.GetEnabled() {
		t.Error("GetDebugConfig = false with RuntimeDebug on, want true")
	}
}

func TestNormalizeIdentityDomains(t *testing.T) {
	t.Parallel()

	domains, err := normalizeIdentityDomains([]string{" Example.com ", "", "corp.example.com"})
	if err != nil {
		t.Fatalf("normalizeIdentityDomains returned error: %v", err)
	}
	if len(domains) != 2 || domains[0] != "example.com" || domains[1] != "corp.example.com" {
		t.Fatalf("normalizeIdentityDomains = %v, want [example.com corp.example.com]", domains)
	}

	if _, err := normalizeIdentityDomains([]string{"@example.com"}); err == nil {
		t.Fatal("an entry carrying '@' must be rejected: it can never match")
	} else if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}

func TestNormalizeAllowedLLMProfiles(t *testing.T) {
	t.Parallel()

	profiles, err := normalizeAllowedLLMProfiles([]string{" llm-provider-profiles/a ", "llm-provider-profiles/a", ""})
	if err != nil {
		t.Fatalf("normalizeAllowedLLMProfiles returned error: %v", err)
	}
	if len(profiles) != 1 || profiles[0] != "llm-provider-profiles/a" {
		t.Fatalf("normalizeAllowedLLMProfiles = %v, want [llm-provider-profiles/a]", profiles)
	}

	if _, err := normalizeAllowedLLMProfiles([]string{"openai"}); err == nil {
		t.Fatal("a bare id must be rejected: the setting stores resource names")
	} else if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}

func TestApplyWorkspaceProfileUpdateDataQuality(t *testing.T) {
	t.Parallel()

	setting := &storepb.WorkspaceProfileSetting{}
	update := &v1pb.WorkspaceProfileSetting{
		DataQuality: &v1pb.DataQualitySetting{
			EnableLlmAnalysis:        true,
			LlmProviderProfile:       " llm-provider-profiles/fast ",
			StoreFailureSamples:      true,
			FailureSampleLimit:       25,
			DefaultScheduleInterval:  durationpb.New(2 * time.Hour),
			DailyExecutionLimit:      100,
			DailyColumnAnalysisLimit: 10,
			MaxColumnsPerAnalysis:    12,
			SendSampleValuesToLlm:    true,
		},
	}

	require.NoError(t, applyWorkspaceProfileUpdate(setting, update, []string{"data_quality"}))
	require.NotNil(t, setting.DataQuality)
	require.True(t, setting.DataQuality.GetEnableLlmAnalysis())
	require.Equal(t, "llm-provider-profiles/fast", setting.DataQuality.GetLlmProviderProfile(), "the stored provider is trimmed")
	require.Equal(t, int32(25), setting.DataQuality.GetFailureSampleLimit())
	require.Equal(t, int32(100), setting.DataQuality.GetDailyExecutionLimit())
	require.Equal(t, int32(10), setting.DataQuality.GetDailyColumnAnalysisLimit())
	require.Equal(t, int32(12), setting.DataQuality.GetMaxColumnsPerAnalysis())
	require.Equal(t, 2*time.Hour, setting.DataQuality.GetDefaultScheduleInterval().AsDuration())
	require.True(t, setting.DataQuality.GetSendSampleValuesToLlm())
}

// The provider is checked against the allowed list after every masked field was
// applied, so the result does not depend on the mask's order.
func TestApplyWorkspaceProfileUpdateDataQualityProviderIsOrderIndependent(t *testing.T) {
	t.Parallel()

	update := &v1pb.WorkspaceProfileSetting{
		AllowedLlmProviderProfiles: []string{"llm-provider-profiles/fast"},
		DataQuality:                &v1pb.DataQualitySetting{LlmProviderProfile: "llm-provider-profiles/fast"},
	}
	for _, paths := range [][]string{
		{"data_quality", "allowed_llm_provider_profiles"},
		{"allowed_llm_provider_profiles", "data_quality"},
	} {
		setting := &storepb.WorkspaceProfileSetting{}
		require.NoError(t, applyWorkspaceProfileUpdate(setting, update, paths), "mask order %v", paths)
		require.Equal(t, "llm-provider-profiles/fast", setting.DataQuality.GetLlmProviderProfile())
	}
}

func TestApplyWorkspaceProfileUpdateDataQualityRejectsUnlistedProvider(t *testing.T) {
	t.Parallel()

	setting := &storepb.WorkspaceProfileSetting{
		AllowedLlmProviderProfiles: []string{"llm-provider-profiles/fast"},
	}
	update := &v1pb.WorkspaceProfileSetting{
		DataQuality: &v1pb.DataQualitySetting{LlmProviderProfile: "llm-provider-profiles/slow"},
	}
	err := applyWorkspaceProfileUpdate(setting, update, []string{"data_quality"})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Nil(t, setting.DataQuality, "a rejected update must not be applied")
}

// A masked but absent data_quality message clears the settings, which reads back
// as the defaults.
func TestApplyWorkspaceProfileUpdateDataQualityClearsWhenMessageIsAbsent(t *testing.T) {
	t.Parallel()

	setting := &storepb.WorkspaceProfileSetting{DataQuality: &storepb.DataQualitySetting{FailureSampleLimit: 10}}
	require.NoError(t, applyWorkspaceProfileUpdate(setting, &v1pb.WorkspaceProfileSetting{}, []string{"data_quality"}))
	require.Nil(t, setting.DataQuality)
}

func TestNormalizeDataQualitySettingAcceptsValidValues(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeDataQualitySetting(&v1pb.DataQualitySetting{
		EnableLlmAnalysis:        true,
		LlmProviderProfile:       "llm-provider-profiles/fast",
		StoreFailureSamples:      true,
		FailureSampleLimit:       dataquality.MaxFailureSampleLimit,
		DefaultScheduleInterval:  durationpb.New(dataquality.MinScheduleInterval),
		DailyExecutionLimit:      0,
		DailyColumnAnalysisLimit: 0,
		MaxColumnsPerAnalysis:    0,
		SendSampleValuesToLlm:    true,
	})
	require.NoError(t, err)
	require.Equal(t, int32(dataquality.MaxFailureSampleLimit), normalized.GetFailureSampleLimit())
	require.Equal(t, int32(0), normalized.GetDailyExecutionLimit(), "zero is stored as zero and resolved by the reader")
	require.Equal(t, dataquality.MinScheduleInterval, normalized.GetDefaultScheduleInterval().AsDuration())
	require.True(t, normalized.GetEnableLlmAnalysis())
	require.True(t, normalized.GetSendSampleValuesToLlm())

	// A nil message is a clear operation; an unset duration stays unset.
	cleared, err := normalizeDataQualitySetting(nil)
	require.NoError(t, err)
	require.Nil(t, cleared)
	zero, err := normalizeDataQualitySetting(&v1pb.DataQualitySetting{})
	require.NoError(t, err)
	require.Nil(t, zero.GetDefaultScheduleInterval())
	require.Empty(t, zero.GetLlmProviderProfile())
}

func TestNormalizeDataQualitySettingRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setting *v1pb.DataQualitySetting
		want    string
	}{
		{
			name:    "sample limit above the cap",
			setting: &v1pb.DataQualitySetting{FailureSampleLimit: dataquality.MaxFailureSampleLimit + 1},
			want:    "failure_sample_limit",
		},
		{
			name:    "negative sample limit",
			setting: &v1pb.DataQualitySetting{FailureSampleLimit: -1},
			want:    "failure_sample_limit",
		},
		{
			name:    "schedule below the minimum",
			setting: &v1pb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(dataquality.MinScheduleInterval - time.Minute)},
			want:    "default_schedule_interval",
		},
		{
			name:    "schedule above the maximum",
			setting: &v1pb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(dataquality.MaxScheduleInterval + time.Second)},
			want:    "default_schedule_interval",
		},
		{
			name:    "negative schedule",
			setting: &v1pb.DataQualitySetting{DefaultScheduleInterval: durationpb.New(-time.Minute)},
			want:    "default_schedule_interval",
		},
		{
			name:    "negative daily execution limit",
			setting: &v1pb.DataQualitySetting{DailyExecutionLimit: -1},
			want:    "daily_execution_limit",
		},
		{
			name:    "negative daily column analysis limit",
			setting: &v1pb.DataQualitySetting{DailyColumnAnalysisLimit: -1},
			want:    "daily_column_analysis_limit",
		},
		{
			name:    "negative max columns",
			setting: &v1pb.DataQualitySetting{MaxColumnsPerAnalysis: -1},
			want:    "max_columns_per_analysis",
		},
		{
			name:    "max columns above the cap",
			setting: &v1pb.DataQualitySetting{MaxColumnsPerAnalysis: dataquality.MaxColumnsPerAnalysisLimit + 1},
			want:    "max_columns_per_analysis",
		},
		{
			name:    "bare provider id",
			setting: &v1pb.DataQualitySetting{LlmProviderProfile: "openai"},
			want:    "LLM provider profile",
		},
		{
			name:    "provider name without an id",
			setting: &v1pb.DataQualitySetting{LlmProviderProfile: "llm-provider-profiles/"},
			want:    "LLM provider profile",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeDataQualitySetting(test.setting)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			require.Contains(t, err.Error(), test.want)
		})
	}

	// Zero means "use the default" for the bounded fields and stays accepted; the
	// cap itself is a valid value.
	_, err := normalizeDataQualitySetting(&v1pb.DataQualitySetting{
		FailureSampleLimit:      0,
		DefaultScheduleInterval: durationpb.New(0),
	})
	require.NoError(t, err)
	capped, err := normalizeDataQualitySetting(&v1pb.DataQualitySetting{MaxColumnsPerAnalysis: dataquality.MaxColumnsPerAnalysisLimit})
	require.NoError(t, err)
	require.Equal(t, int32(dataquality.MaxColumnsPerAnalysisLimit), capped.GetMaxColumnsPerAnalysis())
}

func TestValidateDataQualityProviderProfile(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateDataQualityProviderProfile("", nil))
	require.NoError(t, validateDataQualityProviderProfile("llm-provider-profiles/a", nil),
		"an empty allowed list allows every profile")
	require.NoError(t, validateDataQualityProviderProfile("llm-provider-profiles/a", []string{"llm-provider-profiles/a"}))

	require.Equal(t, connect.CodeInvalidArgument,
		connect.CodeOf(validateDataQualityProviderProfile("llm-provider-profiles/b", []string{"llm-provider-profiles/a"})))
	require.Equal(t, connect.CodeInvalidArgument,
		connect.CodeOf(validateDataQualityProviderProfile("a", nil)))
}

func TestConvertDataQualitySettingRoundTrip(t *testing.T) {
	t.Parallel()

	require.Nil(t, convertDataQualitySetting(nil))

	stored := &storepb.DataQualitySetting{
		EnableLlmAnalysis:        true,
		LlmProviderProfile:       "llm-provider-profiles/fast",
		StoreFailureSamples:      true,
		FailureSampleLimit:       30,
		DefaultScheduleInterval:  durationpb.New(30 * time.Minute),
		DailyExecutionLimit:      10000,
		DailyColumnAnalysisLimit: 50,
		MaxColumnsPerAnalysis:    60,
		SendSampleValuesToLlm:    true,
	}

	apiValue := convertDataQualitySetting(stored)
	require.NotNil(t, apiValue)
	require.Equal(t, stored.GetEnableLlmAnalysis(), apiValue.GetEnableLlmAnalysis())
	require.Equal(t, stored.GetLlmProviderProfile(), apiValue.GetLlmProviderProfile())
	require.Equal(t, stored.GetStoreFailureSamples(), apiValue.GetStoreFailureSamples())
	require.Equal(t, stored.GetFailureSampleLimit(), apiValue.GetFailureSampleLimit())
	require.Equal(t, 30*time.Minute, apiValue.GetDefaultScheduleInterval().AsDuration())
	require.Equal(t, stored.GetDailyExecutionLimit(), apiValue.GetDailyExecutionLimit())
	require.Equal(t, stored.GetDailyColumnAnalysisLimit(), apiValue.GetDailyColumnAnalysisLimit())
	require.Equal(t, stored.GetMaxColumnsPerAnalysis(), apiValue.GetMaxColumnsPerAnalysis())
	require.Equal(t, stored.GetSendSampleValuesToLlm(), apiValue.GetSendSampleValuesToLlm())

	// GetWorkspaceProfileSetting returns the field through the workspace convert.
	converted := convertToWorkspaceProfileSetting(&storepb.WorkspaceProfileSetting{DataQuality: stored})
	require.Equal(t, stored.GetFailureSampleLimit(), converted.GetDataQuality().GetFailureSampleLimit())
	require.Equal(t, "llm-provider-profiles/fast", converted.GetDataQuality().GetLlmProviderProfile())

	// And the API form converts back to the same stored shape.
	back, err := normalizeDataQualitySetting(apiValue)
	require.NoError(t, err)
	require.Equal(t, stored.GetEnableLlmAnalysis(), back.GetEnableLlmAnalysis())
	require.Equal(t, stored.GetDefaultScheduleInterval().AsDuration(), back.GetDefaultScheduleInterval().AsDuration())
	require.Equal(t, stored.GetSendSampleValuesToLlm(), back.GetSendSampleValuesToLlm())
}

// The masks that predate data_quality keep their behavior after the merge moved
// into applyWorkspaceProfileUpdate.
func TestApplyWorkspaceProfileUpdateKeepsExistingMasks(t *testing.T) {
	t.Parallel()

	setting := &storepb.WorkspaceProfileSetting{}
	update := &v1pb.WorkspaceProfileSetting{ExternalUrl: "https://example.com/", DisallowSignup: true}
	require.NoError(t, applyWorkspaceProfileUpdate(setting, update, []string{"external_url", "disallow_signup"}))
	require.Equal(t, "https://example.com", setting.GetExternalUrl())
	require.True(t, setting.GetDisallowSignup())

	require.Equal(t, connect.CodeInvalidArgument,
		connect.CodeOf(applyWorkspaceProfileUpdate(setting, update, []string{"nope"})))
	require.Equal(t, connect.CodeInvalidArgument,
		connect.CodeOf(applyWorkspaceProfileUpdate(setting, &v1pb.WorkspaceProfileSetting{OpenlineageRetentionDays: -1}, []string{"openlineage_retention_days"})))
}

// GetWorkspaceProfileSetting returns the effective data quality settings: a
// workspace that never saved them sees the defaults, with no null for the page
// (and the LLM switch) to interpret.
func TestConvertToWorkspaceProfileSettingResolvesDataQualityDefaults(t *testing.T) {
	t.Parallel()

	for _, setting := range []*storepb.WorkspaceProfileSetting{
		nil,
		{},
	} {
		converted := convertToWorkspaceProfileSetting(setting)
		dataQuality := converted.GetDataQuality()
		require.NotNil(t, dataQuality)
		require.False(t, dataQuality.GetEnableLlmAnalysis())
		require.Empty(t, dataQuality.GetLlmProviderProfile())
		require.True(t, dataQuality.GetStoreFailureSamples())
		require.Equal(t, int32(dataquality.DefaultFailureSampleLimit), dataQuality.GetFailureSampleLimit())
		require.Equal(t, dataquality.DefaultScheduleInterval, dataQuality.GetDefaultScheduleInterval().AsDuration())
		require.Equal(t, int32(dataquality.DefaultDailyExecutionLimit), dataQuality.GetDailyExecutionLimit())
		require.Equal(t, int32(dataquality.DefaultDailyColumnAnalysisLimit), dataQuality.GetDailyColumnAnalysisLimit())
		require.Equal(t, int32(dataquality.DefaultMaxColumnsPerAnalysis), dataQuality.GetMaxColumnsPerAnalysis())
		require.False(t, dataQuality.GetSendSampleValuesToLlm())
	}

	// A present message counts as explicitly written booleans (the settings page
	// writes every field), so an empty one turns sample storage off while the
	// numbers still resolve to their defaults.
	explicit := convertToWorkspaceProfileSetting(&storepb.WorkspaceProfileSetting{DataQuality: &storepb.DataQualitySetting{}})
	require.False(t, explicit.GetDataQuality().GetStoreFailureSamples())
	require.Equal(t, int32(dataquality.DefaultFailureSampleLimit), explicit.GetDataQuality().GetFailureSampleLimit())
}

func TestConvertToWorkspaceProfileSettingKeepsExplicitDisabledSamples(t *testing.T) {
	t.Parallel()

	converted := convertToWorkspaceProfileSetting(&storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{
			EnableLlmAnalysis:        true,
			LlmProviderProfile:       "llm-provider-profiles/fast",
			StoreFailureSamples:      false,
			FailureSampleLimit:       30,
			DefaultScheduleInterval:  durationpb.New(2 * time.Hour),
			DailyExecutionLimit:      0,
			DailyColumnAnalysisLimit: 0,
			MaxColumnsPerAnalysis:    0,
		},
	})

	dataQuality := converted.GetDataQuality()
	require.NotNil(t, dataQuality)
	require.False(t, dataQuality.GetStoreFailureSamples(), "an explicit false stays false")
	require.True(t, dataQuality.GetEnableLlmAnalysis())
	require.Equal(t, "llm-provider-profiles/fast", dataQuality.GetLlmProviderProfile())
	require.Equal(t, int32(30), dataQuality.GetFailureSampleLimit())
	require.Equal(t, 2*time.Hour, dataQuality.GetDefaultScheduleInterval().AsDuration())
	// Unset numbers are filled with the defaults.
	require.Equal(t, int32(dataquality.DefaultDailyExecutionLimit), dataQuality.GetDailyExecutionLimit())
	require.Equal(t, int32(dataquality.DefaultDailyColumnAnalysisLimit), dataQuality.GetDailyColumnAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultMaxColumnsPerAnalysis), dataQuality.GetMaxColumnsPerAnalysis())
}
