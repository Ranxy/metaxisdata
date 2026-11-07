package dataquality

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/schedule"
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
	// workspace may configure. They are the schedule package's own bounds, shared
	// with cron expression validation so the two forms cannot drift apart.
	MinScheduleInterval = schedule.MinInterval
	MaxScheduleInterval = schedule.MaxInterval

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

	// DefaultTimeZone is the zone a check's time variables are resolved in when
	// the workspace does not name one.
	DefaultTimeZone = "UTC"

	// DefaultAgentRunnerTokensPerHour is the workspace's hourly token ceiling for
	// runner goals when the setting does not say. A goal has no individual cap.
	DefaultAgentRunnerTokensPerHour int64 = 100_000_000
	// DefaultAgentRunnerWakesPerHour is how many session wakes a workspace may
	// start per hour.
	DefaultAgentRunnerWakesPerHour = 20
	// DefaultAgentRunnerMaxConcurrentSessions is how many sessions may run at once.
	DefaultAgentRunnerMaxConcurrentSessions = 2
	// MaxAgentRunnerConcurrentSessions is the hard concurrency ceiling.
	MaxAgentRunnerConcurrentSessions = 16
	// MaxAgentRunnerTokensPerHour is the hard ceiling of the hourly token budget.
	MaxAgentRunnerTokensPerHour int64 = 1_000_000_000_000
	// MaxAgentRunnerWakesPerHour is the hard ceiling of the hourly wake budget.
	MaxAgentRunnerWakesPerHour = 1000
	// DefaultAgentRunnerCompactionTriggerTokens is the context size that triggers
	// compaction of a session's older messages.
	DefaultAgentRunnerCompactionTriggerTokens = 262_144
	// MinAgentRunnerCompactionTriggerTokens keeps a compaction threshold large
	// enough for a session to make progress between compactions.
	MinAgentRunnerCompactionTriggerTokens = 16_384
	// MaxAgentRunnerCompactionTriggerTokens is the hard ceiling of the threshold.
	MaxAgentRunnerCompactionTriggerTokens = 4_194_304
	// DefaultAgentRunnerIdleWakeSeconds is how long a session sleeps between wakes
	// when no event or person wakes it sooner.
	DefaultAgentRunnerIdleWakeSeconds = 86_400
	// MinAgentRunnerIdleWakeSeconds keeps a session from waking in a hot loop.
	MinAgentRunnerIdleWakeSeconds = 60
	// MaxAgentRunnerIdleWakeSeconds is the hard ceiling of the idle interval.
	MaxAgentRunnerIdleWakeSeconds = 604_800

	// DefaultCheckTimeout is how long one check's query may run when its config
	// does not say.
	DefaultCheckTimeout = 60 * time.Second
	// MaxCheckTimeout is the ceiling a per-check override may name. A batch still
	// ends with its own deadline, so this bounds one query, not one target's run.
	MaxCheckTimeout = 300 * time.Second
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
	// TimeZone is the IANA name the time variables of a check's filter and custom
	// SQL are resolved in. Always populated: DefaultTimeZone when unset.
	TimeZone string
	// AgentAutonomyEnabled is the workspace kill switch for agent writes. Off by
	// default: an agent principal can still read, and no agent write applies.
	AgentAutonomyEnabled bool

	// AgentRunnerEnabled is the workspace switch for the built-in quality agent
	// runner. Off by default.
	AgentRunnerEnabled bool
	// AgentRunnerDatabases is the set of database resource names the runner may see
	// and work on. Empty means none.
	AgentRunnerDatabases []string
	// AgentRunnerTokensPerHour is the workspace's hourly token ceiling for runner
	// goals. Never zero after resolution.
	AgentRunnerTokensPerHour int64
	// AgentRunnerWakesPerHour is how many session wakes the workspace may start per
	// hour.
	AgentRunnerWakesPerHour int
	// AgentRunnerMaxConcurrentSessions is how many sessions may run at once.
	AgentRunnerMaxConcurrentSessions int
	// AgentRunnerCompactionTriggerTokens is the context size that triggers
	// compaction of the session's older messages.
	AgentRunnerCompactionTriggerTokens int
	// AgentRunnerIdleWakeSeconds is how long an idle session sleeps between wakes.
	AgentRunnerIdleWakeSeconds int
	// AgentRunnerAutoInvestigateFailures appends a failed execution as an event to
	// the target's session and wakes it.
	AgentRunnerAutoInvestigateFailures bool
}

// DefaultSettings returns the settings of a workspace that never saved any.
func DefaultSettings() Settings {
	return Settings{
		EnableLLMAnalysis:                  false,
		LLMProviderProfile:                 "",
		StoreFailureSamples:                true,
		FailureSampleLimit:                 DefaultFailureSampleLimit,
		DefaultScheduleInterval:            DefaultScheduleInterval,
		DailyExecutionLimit:                DefaultDailyExecutionLimit,
		DailyColumnAnalysisLimit:           DefaultDailyColumnAnalysisLimit,
		DailyLLMAnalysisLimit:              DefaultDailyLLMAnalysisLimit,
		LLMConcurrencyLimit:                DefaultLLMConcurrencyLimit,
		MaxColumnsPerAnalysis:              DefaultMaxColumnsPerAnalysis,
		SendSampleValuesToLLM:              false,
		Digest:                             defaultDigestSchedule(),
		TimeZone:                           DefaultTimeZone,
		AgentRunnerTokensPerHour:           DefaultAgentRunnerTokensPerHour,
		AgentRunnerWakesPerHour:            DefaultAgentRunnerWakesPerHour,
		AgentRunnerMaxConcurrentSessions:   DefaultAgentRunnerMaxConcurrentSessions,
		AgentRunnerCompactionTriggerTokens: DefaultAgentRunnerCompactionTriggerTokens,
		AgentRunnerIdleWakeSeconds:         DefaultAgentRunnerIdleWakeSeconds,
		AgentRunnerAutoInvestigateFailures: true,
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
	settings.TimeZone = timeZoneFromStore(dataQuality.GetTimeZone())
	settings.AgentAutonomyEnabled = dataQuality.GetAgentAutonomyEnabled()
	settings.AgentRunnerEnabled = dataQuality.GetAgentRunnerEnabled()
	settings.AgentRunnerDatabases = normalizeDatabaseRefs(dataQuality.GetAgentRunnerDatabases())
	settings.AgentRunnerTokensPerHour = positiveInt64OrDefault(dataQuality.GetAgentRunnerTokensPerHour(), DefaultAgentRunnerTokensPerHour)
	settings.AgentRunnerWakesPerHour = boundedLimit(int(dataQuality.GetAgentRunnerWakesPerHour()), DefaultAgentRunnerWakesPerHour, MaxAgentRunnerWakesPerHour)
	settings.AgentRunnerMaxConcurrentSessions = boundedLimit(int(dataQuality.GetAgentRunnerMaxConcurrentSessions()), DefaultAgentRunnerMaxConcurrentSessions, MaxAgentRunnerConcurrentSessions)
	settings.AgentRunnerCompactionTriggerTokens = boundedMinMax(int(dataQuality.GetAgentRunnerCompactionTriggerTokens()), DefaultAgentRunnerCompactionTriggerTokens, MinAgentRunnerCompactionTriggerTokens, MaxAgentRunnerCompactionTriggerTokens)
	settings.AgentRunnerIdleWakeSeconds = boundedMinMax(int(dataQuality.GetAgentRunnerIdleWakeSeconds()), DefaultAgentRunnerIdleWakeSeconds, MinAgentRunnerIdleWakeSeconds, MaxAgentRunnerIdleWakeSeconds)
	// The event switch defaults on: a session that is enabled should react to a
	// failed check without someone flipping a second switch. A stored explicit false
	// still turns it off.
	if autoInvestigate := dataQuality.AgentRunnerAutoInvestigateFailures; autoInvestigate != nil {
		settings.AgentRunnerAutoInvestigateFailures = *autoInvestigate
	} else {
		settings.AgentRunnerAutoInvestigateFailures = true
	}
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

// boundedMinMax resolves an integer setting with both a floor and a ceiling: zero
// and negative values fall back to the default, and an out-of-range value is clamped.
func boundedMinMax(value, fallback, minimum, maximum int) int {
	switch {
	case value <= 0:
		return fallback
	case value < minimum:
		return minimum
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

// timeZoneFromStore resolves the stored check time zone leniently: this reads
// whatever a previous version or an out-of-band writer left behind, so an
// unknown name falls back to UTC instead of making an execution fail.
func timeZoneFromStore(zone string) string {
	if zone = strings.TrimSpace(zone); zone != "" {
		if _, err := time.LoadLocation(zone); err == nil {
			return zone
		}
	}
	return DefaultTimeZone
}

// ValidateTimeZone validates the check time zone the settings API is about to
// store. An empty name is valid and means UTC. The name must be UTC or an
// Area/Location IANA zone, the same rule a cron schedule applies, so neither a
// stored zone nor a cron schedule silently depends on the deployment host's zone
// or on a fixed-offset abbreviation.
func ValidateTimeZone(zone string) error {
	if strings.TrimSpace(zone) == "" {
		return nil
	}
	return schedule.ValidateTimeZoneName(zone)
}

// TimeZoneLocation resolves the check time zone a rule's time variables are
// rendered in. It never returns nil: an empty or unresolvable setting reads as
// UTC.
func (s Settings) TimeZoneLocation() *time.Location {
	if zone := strings.TrimSpace(s.TimeZone); zone != "" {
		if location, err := time.LoadLocation(zone); err == nil {
			return location
		}
	}
	return time.UTC
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

// normalizeDatabaseRefs trims and de-duplicates the runner's database allowlist.
func normalizeDatabaseRefs(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// positiveInt64OrDefault resolves a proto3 integer setting: zero and negative values
// fall back to the default.
func positiveInt64OrDefault(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

// AgentRunnerDatabaseEnabled reports whether one database is in the runner's scope.
func (s Settings) AgentRunnerDatabaseEnabled(databaseRef string) bool {
	for _, ref := range s.AgentRunnerDatabases {
		if ref == databaseRef {
			return true
		}
	}
	return false
}
