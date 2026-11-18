package qualityagentrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

func reportConfig(frequency store.QualityAgentReportFrequency, timezone, clock string, weekday int16) *store.QualityAgentConfigMessage {
	return &store.QualityAgentConfigMessage{
		ReportFrequency: frequency,
		ReportTimezone:  timezone,
		ReportTime:      clock,
		ReportWeekday:   weekday,
	}
}

func TestDueReportDailyKeepsLocalWallClock(t *testing.T) {
	t.Parallel()

	config := reportConfig(store.QualityAgentReportFrequencyDaily, "America/New_York", "09:00", 0)
	// The first Sunday after the US fall-back: the period start must stay 09:00
	// local, not 10:00 local, although the window is 25 absolute hours.
	now := time.Date(2026, time.November, 1, 14, 0, 0, 0, time.UTC)
	schedule, ok, err := dueReport(config, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2026-11-01", schedule.periodKey)
	require.Equal(t, "2026-11-01 09:00:00 -0500 EST", schedule.periodEnd.Format("2006-01-02 15:04:05 -0700 MST"))
	require.Equal(t, "2026-10-31 09:00:00 -0400 EDT", schedule.periodStart.Format("2006-01-02 15:04:05 -0700 MST"))
}

func TestDueReportWeeklyUsesTheConfiguredWeekday(t *testing.T) {
	t.Parallel()

	// 1 is Monday in the store enum; a Monday 10:00 local wake reports the Monday
	// 09:00 slot of the same day, not Sunday.
	config := reportConfig(store.QualityAgentReportFrequencyWeekly, "UTC", "09:00", 1)
	now := time.Date(2026, time.November, 2, 10, 0, 0, 0, time.UTC)
	schedule, ok, err := dueReport(config, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Monday, schedule.periodEnd.Weekday())
	require.Equal(t, "2026-W45", schedule.periodKey)
	require.Equal(t, schedule.periodEnd.AddDate(0, 0, -7), schedule.periodStart)

	// Sunday (7) maps to Sunday rather than Saturday.
	config.ReportWeekday = 7
	schedule, ok, err = dueReport(config, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Sunday, schedule.periodEnd.Weekday())
}
