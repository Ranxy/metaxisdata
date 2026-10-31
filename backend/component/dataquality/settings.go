package dataquality

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Defaults and hard limits of the workspace data quality settings. The defaults
// are resolved here rather than trusted from proto3 zero values, because every
// zero value of DataQualitySetting means "use the server default".
const (
	// DefaultFailureSampleLimit is how many failed rows a result keeps when the
	// setting does not say.
	DefaultFailureSampleLimit = 20
	// MaxFailureSampleLimit is the hard ceiling of a result's sample rows.
	MaxFailureSampleLimit = 100

	// DefaultScheduleInterval is the interval a target starts with when it gets
	// its first rule.
	DefaultScheduleInterval = time.Hour
	// MinScheduleInterval and MaxScheduleInterval bound the schedule interval a
	// workspace may configure.
	MinScheduleInterval = 15 * time.Minute
	MaxScheduleInterval = 7 * 24 * time.Hour

	// DefaultDailyExecutionLimit is the workspace's daily run ceiling.
	DefaultDailyExecutionLimit = 10000
	// DefaultDailyColumnAnalysisLimit is the daily column-analysis ceiling.
	DefaultDailyColumnAnalysisLimit = 50
	// MaxDailyColumnAnalysisLimit is the hard ceiling a workspace may configure.
	MaxDailyColumnAnalysisLimit = 1000000
	// DefaultDailyLLMAnalysisLimit is the daily L1+L2 analysis ceiling.
	DefaultDailyLLMAnalysisLimit = 10000
	// MaxDailyLLMAnalysisLimit is the hard ceiling a workspace may configure.
	MaxDailyLLMAnalysisLimit = 1000000
	// DefaultLLMConcurrencyLimit is how many analyses may run at once across
	// both quotas.
	DefaultLLMConcurrencyLimit = 8
	// MaxLLMConcurrencyLimit is the hard concurrency ceiling.
	MaxLLMConcurrencyLimit = 64
	// DefaultMaxColumnsPerAnalysis is the maximum number of columns one analysis
	// covers when the setting does not say.
	DefaultMaxColumnsPerAnalysis = 50
	// MaxColumnsPerAnalysisLimit is the hard ceiling of one analysis' columns.
	MaxColumnsPerAnalysisLimit = 200

	// DefaultDigestDay, DefaultDigestHour and DefaultDigestMinute are when the
	// weekly digest is sent when a workspace enables it without picking a
	// schedule: Monday 09:00.
	DefaultDigestDay    = time.Monday
	DefaultDigestHour   = 9
	DefaultDigestMinute = 0
	// DefaultDigestTimezone is the zone a digest schedule without one is
	// evaluated in.
	DefaultDigestTimezone = "UTC"
)

// DigestSchedule is the resolved weekly digest schedule. Disabled means the
// workspace sends none; the remaining fields are always populated so the
// settings page can show the next slot before the switch is turned on.
type DigestSchedule struct {
	Enabled  bool
	Day      time.Weekday
	Hour     int
	Minute   int
	Timezone string
}

// Settings is the resolved data quality configuration of the workspace: the
// stored settings with every "zero means default" hole filled in. The runner and
// the analysis paths read this instead of the proto message, so no caller has to
// remember which value is a real zero.
type Settings struct {
	EnableLLMAnalysis        bool
	LLMProviderProfile       string
	StoreFailureSamples      bool
	FailureSampleLimit       int
	DefaultScheduleInterval  time.Duration
	DailyExecutionLimit      int
	DailyColumnAnalysisLimit int
	DailyLLMAnalysisLimit    int
	LLMConcurrencyLimit      int
	MaxColumnsPerAnalysis    int
	SendSampleValuesToLLM    bool
	Digest                   DigestSchedule
}

// DefaultSettings returns the settings of a workspace that never saved any.
func DefaultSettings() Settings {
	return Settings{
		EnableLLMAnalysis:        false,
		LLMProviderProfile:       "",
		StoreFailureSamples:      true,
		FailureSampleLimit:       DefaultFailureSampleLimit,
		DefaultScheduleInterval:  DefaultScheduleInterval,
		DailyExecutionLimit:      DefaultDailyExecutionLimit,
		DailyColumnAnalysisLimit: DefaultDailyColumnAnalysisLimit,
		DailyLLMAnalysisLimit:    DefaultDailyLLMAnalysisLimit,
		LLMConcurrencyLimit:      DefaultLLMConcurrencyLimit,
		MaxColumnsPerAnalysis:    DefaultMaxColumnsPerAnalysis,
		SendSampleValuesToLLM:    false,
		Digest:                   defaultDigestSchedule(),
	}
}

// SettingsFromProfile resolves the stored profile into Settings.
//
// The numeric fields follow the proto contract that zero means the default, and
// values outside their documented range are clamped instead of rejected: this
// reads whatever a previous version or an out-of-band writer left behind, and a
// settings row must never make the runner fail. The booleans are read from the
// data_quality message as they are, because the settings page always writes them
// explicitly and a false store_failure_samples is meaningfully different from an
// unset message: only when the message is absent do the boolean defaults apply.
func SettingsFromProfile(profile *storepb.WorkspaceProfileSetting) Settings {
	dataQuality := profile.GetDataQuality()
	if dataQuality == nil {
		return DefaultSettings()
	}

	settings := DefaultSettings()
	settings.EnableLLMAnalysis = dataQuality.GetEnableLlmAnalysis()
	settings.LLMProviderProfile = strings.TrimSpace(dataQuality.GetLlmProviderProfile())
	settings.StoreFailureSamples = dataQuality.GetStoreFailureSamples()
	settings.SendSampleValuesToLLM = dataQuality.GetSendSampleValuesToLlm()
	settings.FailureSampleLimit = normalizeFailureSampleLimit(dataQuality.GetFailureSampleLimit())
	settings.DefaultScheduleInterval = normalizeScheduleInterval(dataQuality.GetDefaultScheduleInterval().AsDuration())
	settings.DailyExecutionLimit = positiveOrDefault(dataQuality.GetDailyExecutionLimit(), DefaultDailyExecutionLimit)
	settings.DailyColumnAnalysisLimit = boundedLimit(int(dataQuality.GetDailyColumnAnalysisLimit()), DefaultDailyColumnAnalysisLimit, MaxDailyColumnAnalysisLimit)
	settings.DailyLLMAnalysisLimit = boundedLimit(int(dataQuality.GetDailyLlmAnalysisLimit()), DefaultDailyLLMAnalysisLimit, MaxDailyLLMAnalysisLimit)
	settings.LLMConcurrencyLimit = boundedLimit(int(dataQuality.GetLlmConcurrencyLimit()), DefaultLLMConcurrencyLimit, MaxLLMConcurrencyLimit)
	settings.MaxColumnsPerAnalysis = positiveOrDefault(dataQuality.GetMaxColumnsPerAnalysis(), DefaultMaxColumnsPerAnalysis)
	settings.Digest = DigestScheduleFromStore(dataQuality.GetWeeklyDigest())
	return settings
}

// LoadSettings reads the workspace profile setting and resolves it into Settings.
// A workspace that has not saved the setting, or whose stored value is empty,
// gets the defaults; it is not an error.
func LoadSettings(ctx context.Context, s *store.Store) (Settings, error) {
	if s == nil {
		return Settings{}, common.Errorf(common.Internal, "cannot load data quality settings without a store")
	}
	setting, err := s.GetSetting(ctx, storepb.SettingName_WORKSPACE_PROFILE)
	if err != nil {
		return Settings{}, errors.Wrap(err, "failed to get workspace profile setting")
	}
	if setting == nil {
		return DefaultSettings(), nil
	}
	return settingsFromValue(setting.Value)
}

// settingsFromValue resolves a stored workspace profile payload, which is the
// protojson of metaxisdata.store.WorkspaceProfileSetting. An empty payload means
// the defaults.
func settingsFromValue(value string) (Settings, error) {
	if strings.TrimSpace(value) == "" {
		return DefaultSettings(), nil
	}
	profile := new(storepb.WorkspaceProfileSetting)
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(value), profile); err != nil {
		return Settings{}, errors.Wrap(err, "failed to unmarshal workspace profile setting")
	}
	return SettingsFromProfile(profile), nil
}

func normalizeFailureSampleLimit(limit int32) int {
	switch {
	case limit <= 0:
		return DefaultFailureSampleLimit
	case limit > MaxFailureSampleLimit:
		return MaxFailureSampleLimit
	default:
		return int(limit)
	}
}

func normalizeScheduleInterval(interval time.Duration) time.Duration {
	switch {
	case interval <= 0:
		return DefaultScheduleInterval
	case interval < MinScheduleInterval:
		return MinScheduleInterval
	case interval > MaxScheduleInterval:
		return MaxScheduleInterval
	default:
		return interval
	}
}

// positiveOrDefault resolves a proto3 integer setting: zero and negative values
// fall back to the default.
func positiveOrDefault(value, fallback int32) int {
	if value <= 0 {
		return int(fallback)
	}
	return int(value)
}

// boundedLimit resolves an integer setting with a hard ceiling: zero and negative
// values fall back to the default, and a value above the maximum is clamped. It is
// shared by the settings reader and the quota, so a stored out-of-band value and a
// caller-built limit resolve the same way.
func boundedLimit(value, fallback, maximum int) int {
	switch {
	case value <= 0:
		return fallback
	case value > maximum:
		return maximum
	default:
		return value
	}
}

// defaultDigestSchedule is the schedule of a workspace that never configured one:
// disabled, Monday 09:00 UTC.
func defaultDigestSchedule() DigestSchedule {
	return DigestSchedule{
		Enabled:  false,
		Day:      DefaultDigestDay,
		Hour:     DefaultDigestHour,
		Minute:   DefaultDigestMinute,
		Timezone: DefaultDigestTimezone,
	}
}

// DigestScheduleFromStore resolves the stored digest setting leniently. This
// reads whatever a previous version or an out-of-band writer left behind, so an
// unknown weekday, clock or time zone falls back to the default instead of
// making the digest runner fail; the enabled flag is kept as stored.
func DigestScheduleFromStore(setting *storepb.QualityDigestSetting) DigestSchedule {
	schedule := defaultDigestSchedule()
	if setting == nil {
		return schedule
	}
	schedule.Enabled = setting.GetEnabled()
	if day, ok := digestWeekday(setting.GetDayOfWeek()); ok {
		schedule.Day = day
	}
	if hour, minute, ok := digestClock(setting.GetTime()); ok {
		schedule.Hour = hour
		schedule.Minute = minute
	}
	if timezone := strings.TrimSpace(setting.GetTimezone()); timezone != "" {
		if _, err := time.LoadLocation(timezone); err == nil {
			schedule.Timezone = timezone
		}
	}
	return schedule
}

// ValidateDigestSetting validates a digest setting the settings API is about to
// store. A disabled schedule may keep empty fields (the resolver fills the
// defaults); an enabled one must say when and in which zone it runs.
func ValidateDigestSetting(setting *storepb.QualityDigestSetting) error {
	if setting == nil {
		return nil
	}
	enabled := setting.GetEnabled()
	if _, ok := digestWeekday(setting.GetDayOfWeek()); !ok && (enabled || setting.GetDayOfWeek() != storepb.DayOfWeek_DAY_OF_WEEK_UNSPECIFIED) {
		return errors.New("weekly_digest.day_of_week must be a weekday from MONDAY to SUNDAY")
	}
	if value := strings.TrimSpace(setting.GetTime()); value != "" {
		if _, _, ok := digestClock(value); !ok {
			return errors.New(`weekly_digest.time must be "HH:MM"`)
		}
	} else if enabled {
		return errors.New(`weekly_digest.time is required while the digest is enabled, for example "09:00"`)
	}
	if timezone := strings.TrimSpace(setting.GetTimezone()); timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return errors.Errorf("weekly_digest.timezone %q is not a valid IANA time zone", timezone)
		}
	} else if enabled {
		return errors.New(`weekly_digest.timezone is required while the digest is enabled, for example "Asia/Shanghai"`)
	}
	return nil
}

// digestWeekday maps the proto's ISO 8601 weekday (Monday 1 .. Sunday 7) to the
// standard library's weekday.
func digestWeekday(day storepb.DayOfWeek) (time.Weekday, bool) {
	switch day {
	case storepb.DayOfWeek_DAY_OF_WEEK_MONDAY:
		return time.Monday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_TUESDAY:
		return time.Tuesday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY:
		return time.Wednesday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_THURSDAY:
		return time.Thursday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_FRIDAY:
		return time.Friday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_SATURDAY:
		return time.Saturday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_SUNDAY:
		return time.Sunday, true
	case storepb.DayOfWeek_DAY_OF_WEEK_UNSPECIFIED:
		return 0, false
	default:
		return 0, false
	}
}

// digestClock reads the settings' "HH:MM" form.
func digestClock(value string) (hour, minute int, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// DigestDayOfWeek is the inverse of digestWeekday, for callers that write the
// resolved schedule back into a proto message.
func DigestDayOfWeek(day time.Weekday) storepb.DayOfWeek {
	switch day {
	case time.Monday:
		return storepb.DayOfWeek_DAY_OF_WEEK_MONDAY
	case time.Tuesday:
		return storepb.DayOfWeek_DAY_OF_WEEK_TUESDAY
	case time.Wednesday:
		return storepb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY
	case time.Thursday:
		return storepb.DayOfWeek_DAY_OF_WEEK_THURSDAY
	case time.Friday:
		return storepb.DayOfWeek_DAY_OF_WEEK_FRIDAY
	case time.Saturday:
		return storepb.DayOfWeek_DAY_OF_WEEK_SATURDAY
	case time.Sunday:
		return storepb.DayOfWeek_DAY_OF_WEEK_SUNDAY
	default:
		return storepb.DayOfWeek_DAY_OF_WEEK_UNSPECIFIED
	}
}
