package v1

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
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

// A client that predates the weekly digest sends the whole data_quality message
// without weekly_digest. Replacing the message wholesale would silently turn a
// configured digest off, so an omitted digest keeps the stored one; sending it
// explicitly replaces it.
func TestApplyWorkspaceProfileUpdateKeepsOmittedWeeklyDigest(t *testing.T) {
	t.Parallel()

	stored := &storepb.QualityDigestSetting{
		Enabled:   true,
		DayOfWeek: storepb.DayOfWeek_DAY_OF_WEEK_TUESDAY,
		Time:      "07:30",
		Timezone:  "Asia/Shanghai",
	}
	setting := &storepb.WorkspaceProfileSetting{DataQuality: &storepb.DataQualitySetting{
		FailureSampleLimit: 10,
		WeeklyDigest:       stored,
	}}

	// The legacy-shaped update names data_quality but has no weekly_digest.
	require.NoError(t, applyWorkspaceProfileUpdate(setting, &v1pb.WorkspaceProfileSetting{
		DataQuality: &v1pb.DataQualitySetting{FailureSampleLimit: 20},
	}, []string{"data_quality"}))
	require.Equal(t, int32(20), setting.GetDataQuality().GetFailureSampleLimit())
	require.Equal(t, stored, setting.GetDataQuality().GetWeeklyDigest(), "the omitted digest must survive")

	// Sending it explicitly replaces it, including turning it off.
	require.NoError(t, applyWorkspaceProfileUpdate(setting, &v1pb.WorkspaceProfileSetting{
		DataQuality: &v1pb.DataQualitySetting{
			FailureSampleLimit: 20,
			WeeklyDigest:       &v1pb.QualityDigestSetting{},
		},
	}, []string{"data_quality"}))
	require.True(t, setting.GetDataQuality().GetWeeklyDigest() != nil, "an explicit empty digest is stored, not dropped")
	require.False(t, setting.GetDataQuality().GetWeeklyDigest().GetEnabled())
	require.Empty(t, setting.GetDataQuality().GetWeeklyDigest().GetDayOfWeek())
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
		DailyLlmAnalysisLimit:    dataquality.MaxDailyLLMAnalysisLimit,
		LlmConcurrencyLimit:      dataquality.MaxLLMConcurrencyLimit,
		MaxColumnsPerAnalysis:    0,
		SendSampleValuesToLlm:    true,
		WeeklyDigest: &v1pb.QualityDigestSetting{
			Enabled:   true,
			DayOfWeek: v1pb.DayOfWeek_DAY_OF_WEEK_FRIDAY,
			Time:      " 18:30 ",
			Timezone:  " Asia/Shanghai ",
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(dataquality.MaxFailureSampleLimit), normalized.GetFailureSampleLimit())
	require.Equal(t, int32(0), normalized.GetDailyExecutionLimit(), "zero is stored as zero and resolved by the reader")
	require.Equal(t, int32(dataquality.MaxDailyLLMAnalysisLimit), normalized.GetDailyLlmAnalysisLimit())
	require.Equal(t, int32(dataquality.MaxLLMConcurrencyLimit), normalized.GetLlmConcurrencyLimit())
	require.Equal(t, dataquality.MinScheduleInterval, normalized.GetDefaultScheduleInterval().AsDuration())
	require.True(t, normalized.GetEnableLlmAnalysis())
	require.True(t, normalized.GetSendSampleValuesToLlm())
	require.Equal(t, &storepb.QualityDigestSetting{
		Enabled:   true,
		DayOfWeek: storepb.DayOfWeek_DAY_OF_WEEK_FRIDAY,
		Time:      "18:30",
		Timezone:  "Asia/Shanghai",
	}, normalized.GetWeeklyDigest())

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
			name:    "daily column analysis limit above the cap",
			setting: &v1pb.DataQualitySetting{DailyColumnAnalysisLimit: dataquality.MaxDailyColumnAnalysisLimit + 1},
			want:    "daily_column_analysis_limit",
		},
		{
			name:    "negative daily LLM analysis limit",
			setting: &v1pb.DataQualitySetting{DailyLlmAnalysisLimit: -1},
			want:    "daily_llm_analysis_limit",
		},
		{
			name:    "daily LLM analysis limit above the cap",
			setting: &v1pb.DataQualitySetting{DailyLlmAnalysisLimit: dataquality.MaxDailyLLMAnalysisLimit + 1},
			want:    "daily_llm_analysis_limit",
		},
		{
			name:    "negative LLM concurrency limit",
			setting: &v1pb.DataQualitySetting{LlmConcurrencyLimit: -1},
			want:    "llm_concurrency_limit",
		},
		{
			name:    "LLM concurrency limit above the cap",
			setting: &v1pb.DataQualitySetting{LlmConcurrencyLimit: dataquality.MaxLLMConcurrencyLimit + 1},
			want:    "llm_concurrency_limit",
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
		{
			name: "weekly digest enabled without a weekday",
			setting: &v1pb.DataQualitySetting{WeeklyDigest: &v1pb.QualityDigestSetting{
				Enabled: true, Time: "09:00", Timezone: "UTC",
			}},
			want: "weekly_digest.day_of_week",
		},
		{
			name: "weekly digest enabled without a clock",
			setting: &v1pb.DataQualitySetting{WeeklyDigest: &v1pb.QualityDigestSetting{
				Enabled: true, DayOfWeek: v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, Timezone: "UTC",
			}},
			want: "weekly_digest.time",
		},
		{
			name: "weekly digest with a malformed clock",
			setting: &v1pb.DataQualitySetting{WeeklyDigest: &v1pb.QualityDigestSetting{
				DayOfWeek: v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, Time: "9:00",
			}},
			want: "weekly_digest.time",
		},
		{
			name: "weekly digest enabled without a zone",
			setting: &v1pb.DataQualitySetting{WeeklyDigest: &v1pb.QualityDigestSetting{
				Enabled: true, DayOfWeek: v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, Time: "09:00",
			}},
			want: "weekly_digest.timezone",
		},
		{
			name: "weekly digest with an unknown zone",
			setting: &v1pb.DataQualitySetting{WeeklyDigest: &v1pb.QualityDigestSetting{
				DayOfWeek: v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, Time: "09:00", Timezone: "Mars/Olympus",
			}},
			want: "weekly_digest.timezone",
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

	// The legacy L3 ceiling accepts zero and exactly its cap.
	_, err = normalizeDataQualitySetting(&v1pb.DataQualitySetting{DailyColumnAnalysisLimit: 0})
	require.NoError(t, err)
	columnCapped, err := normalizeDataQualitySetting(&v1pb.DataQualitySetting{DailyColumnAnalysisLimit: dataquality.MaxDailyColumnAnalysisLimit})
	require.NoError(t, err)
	require.Equal(t, int32(dataquality.MaxDailyColumnAnalysisLimit), columnCapped.GetDailyColumnAnalysisLimit())
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
		DailyLlmAnalysisLimit:    25000,
		LlmConcurrencyLimit:      12,
		MaxColumnsPerAnalysis:    60,
		SendSampleValuesToLlm:    true,
		WeeklyDigest: &storepb.QualityDigestSetting{
			Enabled:   true,
			DayOfWeek: storepb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY,
			Time:      "08:15",
			Timezone:  "Europe/Paris",
		},
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
	require.Equal(t, stored.GetDailyLlmAnalysisLimit(), apiValue.GetDailyLlmAnalysisLimit())
	require.Equal(t, stored.GetLlmConcurrencyLimit(), apiValue.GetLlmConcurrencyLimit())
	require.Equal(t, stored.GetMaxColumnsPerAnalysis(), apiValue.GetMaxColumnsPerAnalysis())
	require.Equal(t, stored.GetSendSampleValuesToLlm(), apiValue.GetSendSampleValuesToLlm())
	require.Equal(t, v1pb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY, apiValue.GetWeeklyDigest().GetDayOfWeek())
	require.Equal(t, "08:15", apiValue.GetWeeklyDigest().GetTime())
	require.Equal(t, "Europe/Paris", apiValue.GetWeeklyDigest().GetTimezone())

	// GetWorkspaceProfileSetting returns the field through the workspace convert.
	converted := convertToWorkspaceProfileSetting(&storepb.WorkspaceProfileSetting{DataQuality: stored})
	require.Equal(t, stored.GetFailureSampleLimit(), converted.GetDataQuality().GetFailureSampleLimit())
	require.Equal(t, "llm-provider-profiles/fast", converted.GetDataQuality().GetLlmProviderProfile())

	// And the API form converts back to the same stored shape.
	back, err := normalizeDataQualitySetting(apiValue)
	require.NoError(t, err)
	require.Equal(t, stored.GetEnableLlmAnalysis(), back.GetEnableLlmAnalysis())
	require.Equal(t, stored.GetDefaultScheduleInterval().AsDuration(), back.GetDefaultScheduleInterval().AsDuration())
	require.Equal(t, stored.GetDailyLlmAnalysisLimit(), back.GetDailyLlmAnalysisLimit())
	require.Equal(t, stored.GetLlmConcurrencyLimit(), back.GetLlmConcurrencyLimit())
	require.Equal(t, stored.GetSendSampleValuesToLlm(), back.GetSendSampleValuesToLlm())
	require.Equal(t, stored.GetWeeklyDigest(), back.GetWeeklyDigest())
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
		require.Equal(t, v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, dataQuality.GetWeeklyDigest().GetDayOfWeek())
		require.Equal(t, "09:00", dataQuality.GetWeeklyDigest().GetTime())
		require.Equal(t, "UTC", dataQuality.GetWeeklyDigest().GetTimezone())
		require.False(t, dataQuality.GetWeeklyDigest().GetEnabled())
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

func storedEmailSetting() *storepb.EmailSetting {
	return &storepb.EmailSetting{
		From:     "alerts@example.com",
		FromName: "Metaxisdata",
		Smtp: &storepb.EmailSMTPConfig{
			Host:           "smtp.example.com",
			Port:           587,
			Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS,
			Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN,
			Username:       "alerts",
		},
	}
}

// The plaintext password never enters the stored payload; it is returned next to
// it so the store can seal it.
func TestMergeEmailSettingUpdateKeepsThePasswordOutOfThePayload(t *testing.T) {
	t.Parallel()

	merged, password, err := mergeEmailSettingUpdate(nil, &v1pb.EmailSetting{
		From:     " alerts@example.com ",
		FromName: "Alerts",
		Smtp: &v1pb.SMTPConfig{
			Host:           " smtp.example.com ",
			Port:           587,
			Encryption:     v1pb.SMTPEncryption_SMTP_ENCRYPTION_STARTTLS,
			Authentication: v1pb.SMTPAuthentication_SMTP_AUTHENTICATION_PLAIN,
			Username:       " alerts ",
			Password:       "s3cret",
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "alerts@example.com", merged.GetFrom())
	require.Equal(t, "Alerts", merged.GetFromName())
	require.Equal(t, "smtp.example.com", merged.GetSmtp().GetHost())
	require.Equal(t, "alerts", merged.GetSmtp().GetUsername())
	require.Empty(t, merged.GetSmtp().GetPassword(), "the payload never carries the plaintext")
	require.NotNil(t, password)
	require.Equal(t, "s3cret", *password)
}

// A masked update that names no new password keeps the stored one, so rotating
// the server address does not require re-entering the password.
func TestMergeEmailSettingUpdateKeepsTheStoredPassword(t *testing.T) {
	t.Parallel()

	existing := &store.EmailSettingMessage{Setting: storedEmailSetting(), HasPassword: true}
	merged, password, err := mergeEmailSettingUpdate(existing, &v1pb.EmailSetting{FromName: "Renamed"}, []string{"from_name"})
	require.NoError(t, err)
	require.Nil(t, password)
	require.Equal(t, "Renamed", merged.GetFromName())
	require.Equal(t, "alerts@example.com", merged.GetFrom())
}

func TestMergeEmailSettingUpdatePasswordRotation(t *testing.T) {
	t.Parallel()

	existing := &store.EmailSettingMessage{Setting: storedEmailSetting(), HasPassword: true}
	_, password, err := mergeEmailSettingUpdate(existing, &v1pb.EmailSetting{
		Smtp: &v1pb.SMTPConfig{Password: "new-secret"},
	}, []string{"smtp.password"})
	require.NoError(t, err)
	require.NotNil(t, password)
	require.Equal(t, "new-secret", *password)
}

// Clearing the From address and the host is how an administrator turns email
// delivery off; the RPC then deletes the setting instead of storing a
// half-configured server.
func TestMergeEmailSettingUpdateClearsEverything(t *testing.T) {
	t.Parallel()

	existing := &store.EmailSettingMessage{Setting: storedEmailSetting(), HasPassword: true}
	merged, password, err := mergeEmailSettingUpdate(existing, &v1pb.EmailSetting{
		Smtp: &v1pb.SMTPConfig{},
	}, nil)
	require.NoError(t, err)
	require.Nil(t, password)
	require.Empty(t, merged.GetFrom())
	require.Empty(t, merged.GetSmtp().GetHost())
}

func TestMergeEmailSettingUpdateRejectsIncompleteSettings(t *testing.T) {
	t.Parallel()

	existing := &store.EmailSettingMessage{Setting: storedEmailSetting(), HasPassword: true}
	tests := []struct {
		name     string
		existing *store.EmailSettingMessage
		update   *v1pb.EmailSetting
		paths    []string
	}{
		{
			name: "invalid from",
			update: &v1pb.EmailSetting{From: "not-an-address", Smtp: &v1pb.SMTPConfig{
				Host: "smtp.example.com", Port: 587,
				Encryption:     v1pb.SMTPEncryption_SMTP_ENCRYPTION_STARTTLS,
				Authentication: v1pb.SMTPAuthentication_SMTP_AUTHENTICATION_NONE,
			}},
		},
		{
			name: "authentication needs a password",
			update: &v1pb.EmailSetting{From: "alerts@example.com", Smtp: &v1pb.SMTPConfig{
				Host: "smtp.example.com", Port: 587,
				Encryption:     v1pb.SMTPEncryption_SMTP_ENCRYPTION_STARTTLS,
				Authentication: v1pb.SMTPAuthentication_SMTP_AUTHENTICATION_PLAIN,
				Username:       "alerts",
			}},
		},
		{
			name: "authentication needs encryption",
			update: &v1pb.EmailSetting{From: "alerts@example.com", Smtp: &v1pb.SMTPConfig{
				Host: "smtp.example.com", Port: 25,
				Encryption:     v1pb.SMTPEncryption_SMTP_ENCRYPTION_NONE,
				Authentication: v1pb.SMTPAuthentication_SMTP_AUTHENTICATION_PLAIN,
				Username:       "alerts",
				Password:       "s3cret",
			}},
		},
		{
			name:     "from cannot be cleared while the host stays",
			existing: existing,
			update:   &v1pb.EmailSetting{},
			paths:    []string{"from"},
		},
		{
			name:     "unsupported mask",
			existing: existing,
			update:   &v1pb.EmailSetting{},
			paths:    []string{"surprise"},
		},
	}
	for _, tc := range tests {
		_, _, err := mergeEmailSettingUpdate(tc.existing, tc.update, tc.paths)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), tc.name)
	}
}

func TestConvertToV1EmailSetting(t *testing.T) {
	t.Parallel()

	require.Empty(t, convertToV1EmailSetting(nil).GetFrom())

	converted := convertToV1EmailSetting(&store.EmailSettingMessage{Setting: storedEmailSetting(), HasPassword: true})
	require.Equal(t, "alerts@example.com", converted.GetFrom())
	require.Equal(t, "Metaxisdata", converted.GetFromName())
	require.True(t, converted.GetPasswordSet())
	require.Empty(t, converted.GetSmtp().GetPassword(), "the password is write-only")
	require.Equal(t, int32(587), converted.GetSmtp().GetPort())
	require.Equal(t, v1pb.SMTPEncryption_SMTP_ENCRYPTION_STARTTLS, converted.GetSmtp().GetEncryption())
}

func TestValidateEmailSettingAcceptsAnUnencryptedLoginlessServer(t *testing.T) {
	t.Parallel()

	setting := &storepb.EmailSetting{
		From: "alerts@example.com",
		Smtp: &storepb.EmailSMTPConfig{
			Host:           "mail.internal",
			Port:           25,
			Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
			Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE,
		},
	}
	require.NoError(t, validateEmailSetting(setting, false))

	// Clearing the From address and the host is the off switch, and the remaining
	// fields do not have to be zeroed as well.
	require.NoError(t, validateEmailSetting(&storepb.EmailSetting{
		Smtp: &storepb.EmailSMTPConfig{
			Port:           587,
			Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS,
			Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN,
		},
	}, true))
}

func TestMergeQuietHoursSettingUpdate(t *testing.T) {
	t.Parallel()

	setting, err := mergeQuietHoursSettingUpdate(nil, &v1pb.QuietHoursSetting{
		Enabled:  true,
		Timezone: " Asia/Shanghai ",
		Windows: []*v1pb.QuietWindow{{
			Days:  []v1pb.DayOfWeek{v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY, v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY},
			Start: "22:00",
			End:   "08:00",
		}},
		MaintenanceWindows: []*v1pb.MaintenanceWindow{{
			Start: timestamppb.New(time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)),
			End:   timestamppb.New(time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)),
		}},
	}, nil)
	require.NoError(t, err)
	require.True(t, setting.GetEnabled())
	require.Equal(t, "Asia/Shanghai", setting.GetTimezone())
	require.Len(t, setting.GetWindows(), 1)
	require.Equal(t, []storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, setting.GetWindows()[0].GetDays())
	require.Len(t, setting.GetMaintenanceWindows(), 1)

	// A masked update keeps everything the mask does not name.
	merged, err := mergeQuietHoursSettingUpdate(setting, &v1pb.QuietHoursSetting{Enabled: false}, []string{"enabled"})
	require.NoError(t, err)
	require.False(t, merged.GetEnabled())
	require.Equal(t, "Asia/Shanghai", merged.GetTimezone())
	require.Len(t, merged.GetWindows(), 1)
}

func TestMergeQuietHoursSettingUpdateRejectsIncompleteSchedules(t *testing.T) {
	t.Parallel()

	valid := &v1pb.QuietHoursSetting{
		Enabled:  true,
		Timezone: "UTC",
		Windows:  []*v1pb.QuietWindow{{Days: []v1pb.DayOfWeek{v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY}, Start: "22:00", End: "08:00"}},
	}
	tests := map[string]*v1pb.QuietHoursSetting{
		"no timezone":        {Enabled: true, Windows: valid.GetWindows()},
		"invalid timezone":   {Enabled: true, Timezone: "Mars/Olympus", Windows: valid.GetWindows()},
		"no windows":         {Enabled: true, Timezone: "UTC"},
		"no days":            {Enabled: true, Timezone: "UTC", Windows: []*v1pb.QuietWindow{{Start: "22:00", End: "08:00"}}},
		"invalid day":        {Enabled: true, Timezone: "UTC", Windows: []*v1pb.QuietWindow{{Days: []v1pb.DayOfWeek{v1pb.DayOfWeek(9)}, Start: "22:00", End: "08:00"}}},
		"invalid clock":      {Enabled: true, Timezone: "UTC", Windows: []*v1pb.QuietWindow{{Days: valid.Windows[0].Days, Start: "25:00", End: "08:00"}}},
		"same start and end": {Enabled: true, Timezone: "UTC", Windows: []*v1pb.QuietWindow{{Days: valid.Windows[0].Days, Start: "22:00", End: "22:00"}}},
		"maintenance backwards": {Enabled: true, Timezone: "UTC", MaintenanceWindows: []*v1pb.MaintenanceWindow{{
			Start: timestamppb.New(time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)),
			End:   timestamppb.New(time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)),
		}}},
	}
	for name, setting := range tests {
		_, err := mergeQuietHoursSettingUpdate(nil, setting, nil)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
	}

	_, err := mergeQuietHoursSettingUpdate(nil, &v1pb.QuietHoursSetting{}, []string{"surprise"})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Clearing every field is the off switch and passes validation.
	empty, err := mergeQuietHoursSettingUpdate(validSettingForQuietHours(), &v1pb.QuietHoursSetting{}, nil)
	require.NoError(t, err)
	require.Empty(t, empty.GetEnabled())
	require.Empty(t, empty.GetWindows())
}

func validSettingForQuietHours() *storepb.QuietHoursSetting {
	return &storepb.QuietHoursSetting{
		Enabled:  true,
		Timezone: "UTC",
		Windows: []*storepb.QuietWindow{{
			Days:  []storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY},
			Start: "22:00",
			End:   "08:00",
		}},
	}
}

func TestConvertToV1QuietHoursSetting(t *testing.T) {
	t.Parallel()

	require.Empty(t, convertToV1QuietHoursSetting(nil).GetEnabled())

	start := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	setting := validSettingForQuietHours()
	setting.MaintenanceWindows = []*storepb.MaintenanceWindow{{
		Start: timestamppb.New(start),
		End:   timestamppb.New(start.Add(2 * time.Hour)),
	}}
	converted := convertToV1QuietHoursSetting(setting)
	require.True(t, converted.GetEnabled())
	require.Equal(t, "UTC", converted.GetTimezone())
	require.Equal(t, []v1pb.DayOfWeek{v1pb.DayOfWeek_DAY_OF_WEEK_MONDAY}, converted.GetWindows()[0].GetDays())
	require.Equal(t, "22:00", converted.GetWindows()[0].GetStart())
	require.Equal(t, start, converted.GetMaintenanceWindows()[0].GetStart().AsTime())
}
