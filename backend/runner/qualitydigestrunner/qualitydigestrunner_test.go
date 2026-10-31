package qualitydigestrunner

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// scheduleIn builds a digest schedule at hour:minute in timezone.
func scheduleIn(day time.Weekday, hour, minute int, timezone string) dataquality.DigestSchedule {
	return dataquality.DigestSchedule{Enabled: true, Day: day, Hour: hour, Minute: minute, Timezone: timezone}
}

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return location
}

func newYork(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	return location
}

// The most recent scheduled instant at or before now is the digest that is due:
// the schedule's weekday and wall clock are read in its own time zone, a pass
// within the catch-up window still sends a missed slot, and an older one is
// skipped instead of backfilled.
func TestDueDigest(t *testing.T) {
	t.Parallel()

	loc := shanghai(t)
	monday := time.Date(2026, 10, 26, 9, 0, 0, 0, loc) // ISO 2026-W44
	weekly := scheduleIn(time.Monday, 9, 0, "Asia/Shanghai")

	for _, tc := range []struct {
		name      string
		schedule  dataquality.DigestSchedule
		now       time.Time
		due       bool
		until     time.Time
		periodKey string
	}{
		{
			name:     "exactly on the slot",
			schedule: weekly,
			now:      monday,
			due:      true, until: monday, periodKey: "2026-W44",
		},
		{
			name:     "five minutes late is still the same slot",
			schedule: weekly,
			now:      monday.Add(5 * time.Minute),
			due:      true, until: monday, periodKey: "2026-W44",
		},
		{
			name:     "a minute before the slot is not due",
			schedule: weekly,
			now:      monday.Add(-time.Minute),
			due:      false,
		},
		{
			name:     "the tail of the catch-up window still sends",
			schedule: weekly,
			now:      monday.Add(catchUpWindow - time.Minute),
			due:      true, until: monday, periodKey: "2026-W44",
		},
		{
			name:     "past the catch-up window is skipped",
			schedule: weekly,
			now:      monday.Add(catchUpWindow + time.Minute),
			due:      false,
		},
		{
			name:     "a weekday later in the week looks back the whole week",
			schedule: scheduleIn(time.Friday, 9, 0, "Asia/Shanghai"),
			now:      time.Date(2026, 10, 28, 10, 0, 0, 0, loc),
			due:      false,
		},
		{
			name:      "the wall clock is read in the schedule's zone",
			schedule:  scheduleIn(time.Monday, 9, 0, "UTC"),
			now:       time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC),
			due:       true,
			until:     time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC),
			periodKey: "2026-W44",
		},
		{
			name:      "a schedule just ahead of UTC is not due a day early",
			schedule:  scheduleIn(time.Monday, 9, 0, "Pacific/Kiritimati"),
			now:       time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC), // Monday 10:00 in UTC+14
			due:       true,
			until:     time.Date(2026, 10, 26, 9, 0, 0, 0, mustLocation(t, "Pacific/Kiritimati")),
			periodKey: "2026-W44",
		},
		{
			name:      "an unknown zone falls back to UTC",
			schedule:  scheduleIn(time.Monday, 9, 0, "Not/AZone"),
			now:       time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC),
			due:       true,
			until:     time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC),
			periodKey: "2026-W44",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			period := mostRecentDigest(tc.schedule, tc.now)
			due := period.periodKey != "" && period.dueAt(tc.now)
			require.Equal(t, tc.due, due)
			if !tc.due {
				return
			}
			require.True(t, period.until.Equal(tc.until), "until = %s, want %s", period.until, tc.until)
			require.Equal(t, tc.until.Add(-periodLength), period.since)
			require.Equal(t, tc.periodKey, period.periodKey)
		})
	}
}

// A weekly slot whose wall clock disappears in the spring-forward gap resolves to
// a real instant in the same day instead of skipping the week, and the ambiguous
// hour of the fall-back day resolves to the first occurrence. Both keep the ISO
// week the schedule fired in.
func TestDueDigestAcrossDaylightSavingChanges(t *testing.T) {
	t.Parallel()

	loc := newYork(t)
	// 2026-03-08 02:30 does not exist in America/New_York; Go normalizes it to
	// 01:30 EST, still the scheduled day.
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, loc)
	period := mostRecentDigest(scheduleIn(time.Sunday, 2, 30, "America/New_York"), now)
	require.True(t, period.dueAt(now))
	require.True(t, period.until.Equal(time.Date(2026, 3, 8, 1, 30, 0, 0, loc)))
	require.Equal(t, "2026-W10", period.periodKey)
	require.True(t, period.until.Before(time.Date(2026, 3, 8, 12, 0, 0, 0, loc)))

	// 01:30 on 2026-11-01 happens twice; the digest is one period either way.
	now = time.Date(2026, 11, 1, 12, 0, 0, 0, loc)
	period = mostRecentDigest(scheduleIn(time.Sunday, 1, 30, "America/New_York"), now)
	require.True(t, period.dueAt(now))
	require.True(t, period.until.Equal(time.Date(2026, 11, 1, 1, 30, 0, 0, loc)))
	require.Equal(t, "2026-W44", period.periodKey)
}

// The period key names the send week, so two consecutive sends are two periods.
func TestDueDigestPeriodKeysAdvanceWeekly(t *testing.T) {
	t.Parallel()

	loc := shanghai(t)
	first := time.Date(2026, 10, 26, 9, 0, 0, 0, loc)
	weekly := scheduleIn(time.Monday, 9, 0, "Asia/Shanghai")

	period := mostRecentDigest(weekly, first.Add(time.Minute))
	require.Equal(t, "2026-W44", period.periodKey)
	require.True(t, period.dueAt(first.Add(time.Minute)))

	second := first.AddDate(0, 0, 7).Add(time.Minute)
	period = mostRecentDigest(weekly, second)
	require.Equal(t, "2026-W45", period.periodKey)
	require.True(t, period.dueAt(second))

	// An early Sunday send is still the week it ends, not the week it announces.
	sunday := time.Date(2026, 10, 25, 23, 0, 0, 0, loc)
	period = mostRecentDigest(scheduleIn(time.Sunday, 23, 0, "Asia/Shanghai"), sunday)
	require.Equal(t, "2026-W43", period.periodKey)
	require.True(t, period.dueAt(sunday))
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	require.NoError(t, err)
	return location
}

type notifierCall struct {
	periodKey string
	severity  storepb.NotificationSeverity
	detail    *storepb.DataQualityDigestDetail
}

type fakeNotifier struct {
	calls    []notifierCall
	channels int
	err      error
}

func (f *fakeNotifier) EnqueueDataQualityDigest(_ context.Context, periodKey string, severity storepb.NotificationSeverity, detail *storepb.DataQualityDigestDetail) (int, error) {
	f.calls = append(f.calls, notifierCall{periodKey: periodKey, severity: severity, detail: detail})
	if f.err != nil {
		return 0, f.err
	}
	return f.channels, nil
}

type recordCall struct {
	periodKey    string
	channelCount int
}

type fakeStore struct {
	recorded    map[string]bool
	recordCalls []string
	tryCalls    []recordCall
	snapshot    *store.QualityDigestSnapshot
	snapshots   int
	panicOnRead bool
	readErr     error
	snapshotErr error
	recordErr   error
}

func (f *fakeStore) QualityDigestSendRecorded(_ context.Context, periodKey string) (bool, error) {
	if f.panicOnRead {
		panic("store read exploded")
	}
	f.recordCalls = append(f.recordCalls, periodKey)
	if f.readErr != nil {
		return false, f.readErr
	}
	return f.recorded[periodKey], nil
}

func (f *fakeStore) QualityDigestSnapshot(context.Context, time.Time, time.Time) (*store.QualityDigestSnapshot, error) {
	f.snapshots++
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	if f.snapshot == nil {
		return &store.QualityDigestSnapshot{}, nil
	}
	return f.snapshot, nil
}

func (f *fakeStore) TryRecordQualityDigestSend(_ context.Context, periodKey string, channelCount int) (bool, error) {
	f.tryCalls = append(f.tryCalls, recordCall{periodKey: periodKey, channelCount: channelCount})
	if f.recordErr != nil {
		return false, f.recordErr
	}
	if f.recorded == nil {
		f.recorded = map[string]bool{}
	}
	if f.recorded[periodKey] {
		return false, nil
	}
	f.recorded[periodKey] = true
	return true, nil
}

func enabledSettings() dataquality.Settings {
	settings := dataquality.DefaultSettings()
	settings.Digest = scheduleIn(time.Monday, 9, 0, "Asia/Shanghai")
	return settings
}

func newTestRunner(t *testing.T, stores Store, notifier Notifier, settings dataquality.Settings, now time.Time) *Runner {
	t.Helper()
	return &Runner{
		store:    stores,
		notifier: notifier,
		loadSettings: func(context.Context) (dataquality.Settings, error) {
			return settings, nil
		},
		now: func() time.Time { return now },
	}
}

// A due period is aggregated, queued and then recorded. The record call is what
// makes the next tick cheap.
func TestRunnerPassQueuesAndRecordsTheDueDigest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))
	stores := &fakeStore{snapshot: &store.QualityDigestSnapshot{
		TargetCount:         3,
		RuleCount:           9,
		ExecutionCount:      20,
		PassedCount:         18,
		FailedCount:         2,
		OpenIssueCount:      2,
		OpenErrorCount:      1,
		NewIssueCount:       1,
		RecoveredIssueCount: 0,
		Owners: []*store.QualityDigestOwnerMessage{
			{UserID: 7, Name: "Alice", OpenIssueCount: 2, FailingRuleCount: 1},
		},
		Targets: []*store.QualityDigestTargetMessage{
			{DisplayPath: "prod/shop/public/orders", FailedExecutionCount: 2, OpenIssueCount: 1},
		},
	}}
	notifier := &fakeNotifier{channels: 2}
	runner := newTestRunner(t, stores, notifier, enabledSettings(), now)

	runner.pass(context.Background())

	require.Equal(t, []string{"2026-W44"}, stores.recordCalls)
	require.Equal(t, 1, stores.snapshots)
	require.Len(t, notifier.calls, 1)
	call := notifier.calls[0]
	require.Equal(t, "2026-W44", call.periodKey)
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, call.severity)
	require.Equal(t, int32(3), call.detail.GetTargetCount())
	require.NotNil(t, call.detail.GetPeriodStart())
	require.True(t, call.detail.GetPeriodEnd().AsTime().Equal(time.Date(2026, 10, 26, 9, 0, 0, 0, shanghai(t))))
	require.Len(t, call.detail.GetOwners(), 1)
	require.Equal(t, "users/7", call.detail.GetOwners()[0].GetOwner())
	require.Len(t, call.detail.GetTargets(), 1)
	require.Equal(t, "prod/shop/public/orders", call.detail.GetTargets()[0].GetTargetPath())

	require.Equal(t, []recordCall{{periodKey: "2026-W44", channelCount: 2}}, stores.tryCalls)
}

// A disabled digest costs one settings read and nothing else, and a period that
// was already queued is not aggregated again.
func TestRunnerPassWhenNothingIsDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))

	disabled := enabledSettings()
	disabled.Digest.Enabled = false
	stores := &fakeStore{}
	notifier := &fakeNotifier{}
	newTestRunner(t, stores, notifier, disabled, now).pass(context.Background())
	require.Empty(t, stores.recordCalls)
	require.Empty(t, notifier.calls)

	stores = &fakeStore{recorded: map[string]bool{"2026-W44": true}}
	notifier = &fakeNotifier{}
	enabled := newTestRunner(t, stores, notifier, enabledSettings(), now)
	enabled.pass(context.Background())
	require.Equal(t, []string{"2026-W44"}, stores.recordCalls)
	require.Zero(t, stores.snapshots, "a recorded period must not be aggregated")
	require.Empty(t, notifier.calls)

	// Outside the catch-up window the stale period is not sent, but it is probed
	// once and then remembered, so the next tick does not read it again.
	stores = &fakeStore{}
	notifier = &fakeNotifier{}
	stale := newTestRunner(t, stores, notifier, enabledSettings(), now.AddDate(0, 0, 8))
	stale.pass(context.Background())
	require.Equal(t, []string{"2026-W45"}, stores.recordCalls)
	require.Zero(t, stores.snapshots)
	require.Empty(t, notifier.calls)
	stale.pass(context.Background())
	require.Equal(t, []string{"2026-W45"}, stores.recordCalls, "a handled period is not probed again")
}

// A week with no subscribed channel is still a finished period: it is recorded
// with zero channels so the next tick does not aggregate it again.
func TestRunnerPassRecordsZeroChannels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))
	stores := &fakeStore{}
	notifier := &fakeNotifier{channels: 0}
	newTestRunner(t, stores, notifier, enabledSettings(), now).pass(context.Background())

	require.Len(t, notifier.calls, 1)
	require.Equal(t, []recordCall{{periodKey: "2026-W44", channelCount: 0}}, stores.tryCalls)
}

// An enqueue failure must leave the period unrecorded so the next tick retries;
// the retry is harmless because the event id is the period itself.
func TestRunnerPassRetriesAfterEnqueueFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))
	stores := &fakeStore{}
	notifier := &fakeNotifier{channels: 1, err: errors.New("outbox unavailable")}
	runner := newTestRunner(t, stores, notifier, enabledSettings(), now)

	runner.pass(context.Background())
	require.Empty(t, stores.tryCalls, "a failed enqueue must not record the period")

	notifier.err = nil
	runner.pass(context.Background())
	require.Len(t, notifier.calls, 2)
	require.Equal(t, notifier.calls[0].periodKey, notifier.calls[1].periodKey)
	require.Equal(t, []recordCall{{periodKey: "2026-W44", channelCount: 1}}, stores.tryCalls)
}

// A failure before the enqueue leaves the period unrecorded too: aggregation and
// the settings read are retried on the next tick rather than skipped for a week.
func TestRunnerPassLeavesThePeriodOpenOnErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))

	stores := &fakeStore{snapshotErr: errors.New("aggregate failed")}
	notifier := &fakeNotifier{}
	newTestRunner(t, stores, notifier, enabledSettings(), now).pass(context.Background())
	require.Empty(t, notifier.calls)
	require.Empty(t, stores.tryCalls)

	stores = &fakeStore{readErr: errors.New("read failed")}
	newTestRunner(t, stores, notifier, enabledSettings(), now).pass(context.Background())
	require.Zero(t, stores.snapshots)
	require.Empty(t, notifier.calls)

	stores = &fakeStore{recordErr: errors.New("record failed")}
	notifier = &fakeNotifier{channels: 1}
	newTestRunner(t, stores, notifier, enabledSettings(), now).pass(context.Background())
	require.Len(t, notifier.calls, 1)
	require.Len(t, stores.tryCalls, 1)
}

// A settings read failure is logged rather than fatal, and the next tick
// re-reads the setting.
func TestRunnerPassSurvivesASettingsFailure(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	notifier := &fakeNotifier{}
	runner := &Runner{
		store:    stores,
		notifier: notifier,
		loadSettings: func(context.Context) (dataquality.Settings, error) {
			return dataquality.Settings{}, errors.New("setting unavailable")
		},
		now: time.Now,
	}
	runner.pass(context.Background())
	require.Empty(t, stores.recordCalls)
	require.Empty(t, notifier.calls)
}

// A panic inside one pass must not escape: runner.Go's boundary covers the whole
// loop, so without this guard a single bad pass would stop the digest forever.
func TestRunnerPanicIsContainedInOnePass(t *testing.T) {
	t.Parallel()

	runner := newTestRunner(t, &fakeStore{panicOnRead: true}, &fakeNotifier{}, enabledSettings(),
		time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t)))
	require.NotPanics(t, func() { runner.guardPass(context.Background()) })
}

// The envelope's severity is the worst thing the week holds: an errored
// execution or an open error/critical problem is ERROR, a failed check or any
// open problem is WARNING, a clean week is INFO.
func TestDigestSeverity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		snapshot *store.QualityDigestSnapshot
		want     storepb.NotificationSeverity
	}{
		{name: "clean", snapshot: &store.QualityDigestSnapshot{}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO},
		{name: "failed execution", snapshot: &store.QualityDigestSnapshot{FailedCount: 1}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING},
		{name: "open warning", snapshot: &store.QualityDigestSnapshot{OpenIssueCount: 1, OpenWarningCount: 1}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING},
		{name: "errored execution", snapshot: &store.QualityDigestSnapshot{ErroredCount: 1}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR},
		{name: "open error", snapshot: &store.QualityDigestSnapshot{OpenIssueCount: 1, OpenErrorCount: 1}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR},
		{name: "open critical", snapshot: &store.QualityDigestSnapshot{OpenIssueCount: 1, OpenCriticalCount: 1}, want: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, digestSeverity(tc.snapshot))
		})
	}
}

// The detail is a pure mapping: counts, the period's timestamps, the owner
// resource names (with the unassigned bucket left empty) and the target paths.
func TestDigestDetail(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC)
	until := since.Add(periodLength)
	period := digestPeriod{since: since, until: until, periodKey: "2026-W44"}
	detail := digestDetail(period, &store.QualityDigestSnapshot{
		TargetCount:         3,
		RuleCount:           9,
		ExecutionCount:      20,
		PassedCount:         17,
		WarnedCount:         1,
		FailedCount:         1,
		ErroredCount:        1,
		NewIssueCount:       2,
		RecoveredIssueCount: 1,
		OpenIssueCount:      4,
		OpenWarningCount:    2,
		OpenErrorCount:      1,
		OpenCriticalCount:   1,
		Owners: []*store.QualityDigestOwnerMessage{
			{UserID: 7, Name: "Alice", OpenIssueCount: 3, FailingRuleCount: 2},
			{OpenIssueCount: 1, FailingRuleCount: 1},
		},
		Targets: []*store.QualityDigestTargetMessage{
			{DisplayPath: "prod/shop/public/orders", FailedExecutionCount: 1, ErroredExecutionCount: 1, OpenIssueCount: 2},
		},
	}, "Asia/Shanghai")

	require.Equal(t, "Asia/Shanghai", detail.GetTimezone(), "the period label needs the schedule's zone")
	require.True(t, detail.GetPeriodStart().AsTime().Equal(since))
	require.True(t, detail.GetPeriodEnd().AsTime().Equal(until))
	require.Equal(t, int32(20), detail.GetExecutionCount())
	require.Equal(t,
		detail.GetPassedCount()+detail.GetWarnedCount()+detail.GetFailedCount()+detail.GetErroredCount(),
		detail.GetExecutionCount(), "execution_count is the sum of the four verdicts")
	require.Equal(t, int32(1), detail.GetErroredCount())
	require.Equal(t, int32(4), detail.GetOpenIssueCount())
	require.Len(t, detail.GetOwners(), 2)
	require.Equal(t, "users/7", detail.GetOwners()[0].GetOwner())
	require.Equal(t, "Alice", detail.GetOwners()[0].GetOwnerName())
	require.Empty(t, detail.GetOwners()[1].GetOwner(), "zero is the unassigned bucket")
	require.Len(t, detail.GetTargets(), 1)
	require.Equal(t, "prod/shop/public/orders", detail.GetTargets()[0].GetTargetPath())
	require.Equal(t, int32(1), detail.GetTargets()[0].GetErroredExecutionCount())
}

func TestRunnerName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "qualitydigest", (&Runner{}).Name())
}

// digestLogCapture collects this package's log records so a test can assert a
// warning fires once per period instead of once per tick.
type digestLogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*digestLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *digestLogCapture) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record.Clone())
	return nil
}

func (c *digestLogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *digestLogCapture) WithGroup(string) slog.Handler      { return c }

func (c *digestLogCapture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = nil
}

func (c *digestLogCapture) countWarnings(message string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, record := range c.records {
		if record.Level >= slog.LevelWarn && record.Message == message {
			count++
		}
	}
	return count
}

// An outage that outlives the catch-up window skips the week instead of
// backfilling it, and says so exactly once: the period is remembered after the
// warning, so the minute-by-minute pass for the rest of the week stays quiet.
func TestRunnerWarnsOnceAboutAStaleUnrecordedPeriod(t *testing.T) {
	// Not parallel: it replaces the process logger.
	previous := slog.Default()
	capture := &digestLogCapture{}
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t)).AddDate(0, 0, 8)
	stores := &fakeStore{}
	notifier := &fakeNotifier{}
	runner := newTestRunner(t, stores, notifier, enabledSettings(), now)

	runner.pass(context.Background())
	runner.pass(context.Background())

	require.Equal(t, []string{"2026-W45"}, stores.recordCalls)
	require.Zero(t, stores.snapshots)
	require.Empty(t, notifier.calls)
	require.Equal(t, 1, capture.countWarnings("Skipping a stale weekly data quality digest period"))

	// A stale period that was in fact queued has nothing to report: the ordinary
	// case is a digest sent on time and seen again by the next tick.
	capture.reset()
	recordedStores := &fakeStore{recorded: map[string]bool{"2026-W45": true}}
	recordedRunner := newTestRunner(t, recordedStores, &fakeNotifier{}, enabledSettings(), now)
	recordedRunner.pass(context.Background())
	recordedRunner.pass(context.Background())
	require.Equal(t, []string{"2026-W45"}, recordedStores.recordCalls)
	require.Zero(t, capture.countWarnings("Skipping a stale weekly data quality digest period"))
}

// A persistent failure keeps retrying every pass (a transient one must recover
// immediately) but warns once per period and kind, so a week-long outage writes a
// handful of lines instead of one a minute.
func TestRunnerRateLimitsFailureWarnings(t *testing.T) {
	// Not parallel: it replaces the process logger.
	previous := slog.Default()
	capture := &digestLogCapture{}
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	now := time.Date(2026, 10, 26, 9, 5, 0, 0, shanghai(t))
	ctx := context.Background()

	// Aggregation failing every pass: three attempts, one warning.
	stores := &fakeStore{snapshotErr: errors.New("aggregate failed")}
	notifier := &fakeNotifier{}
	runner := newTestRunner(t, stores, notifier, enabledSettings(), now)
	for range 3 {
		runner.pass(ctx)
	}
	require.Equal(t, 3, stores.snapshots, "the retry stays unthrottled")
	require.Equal(t, 1, capture.countWarnings("Failed to aggregate the weekly digest period"))

	// Enqueue failing every pass: its own warning, also once.
	stores = &fakeStore{}
	notifier = &fakeNotifier{channels: 1, err: errors.New("outbox unavailable")}
	runner = newTestRunner(t, stores, notifier, enabledSettings(), now)
	for range 3 {
		runner.pass(ctx)
	}
	require.Len(t, notifier.calls, 3)
	require.Equal(t, 1, capture.countWarnings("Failed to queue the weekly data quality digest"))

	// The failure clearing still records the period on the next pass.
	notifier.err = nil
	runner.pass(ctx)
	require.Equal(t, []recordCall{{periodKey: "2026-W44", channelCount: 1}}, stores.tryCalls)
}
