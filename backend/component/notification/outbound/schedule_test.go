package outbound

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return location
}

func quietSetting(windows ...*storepb.QuietWindow) *storepb.QuietHoursSetting {
	return &storepb.QuietHoursSetting{
		Enabled:  true,
		Timezone: "Asia/Shanghai",
		Windows:  windows,
	}
}

func recurringWindow(days []storepb.DayOfWeek, start, end string) *storepb.QuietWindow {
	return &storepb.QuietWindow{Days: days, Start: start, End: end}
}

func TestQuietUntilRecurringWindow(t *testing.T) {
	t.Parallel()

	location := shanghai(t)
	// 2025-01-06 is a Monday.
	setting := quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "22:00", "23:00"))

	inside := time.Date(2025, 1, 6, 22, 30, 0, 0, location)
	until, quiet := QuietUntil(inside, setting)
	require.True(t, quiet)
	require.Equal(t, time.Date(2025, 1, 6, 23, 0, 0, 0, location), until.In(location))

	before := time.Date(2025, 1, 6, 21, 59, 0, 0, location)
	_, quiet = QuietUntil(before, setting)
	require.False(t, quiet)

	atEnd := time.Date(2025, 1, 6, 23, 0, 0, 0, location)
	_, quiet = QuietUntil(atEnd, setting)
	require.False(t, quiet)

	// Tuesday is outside the Monday-only window.
	_, quiet = QuietUntil(time.Date(2025, 1, 7, 22, 30, 0, 0, location), setting)
	require.False(t, quiet)
}

// An end at or before the start crosses midnight into the following day.
func TestQuietUntilCrossesMidnight(t *testing.T) {
	t.Parallel()

	location := shanghai(t)
	setting := quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "22:00", "02:00"))

	lateMonday := time.Date(2025, 1, 6, 23, 0, 0, 0, location)
	until, quiet := QuietUntil(lateMonday, setting)
	require.True(t, quiet)
	require.Equal(t, time.Date(2025, 1, 7, 2, 0, 0, 0, location), until.In(location))

	// The window still covers early Tuesday, although Tuesday is not listed.
	earlyTuesday := time.Date(2025, 1, 7, 1, 30, 0, 0, location)
	until, quiet = QuietUntil(earlyTuesday, setting)
	require.True(t, quiet)
	require.Equal(t, time.Date(2025, 1, 7, 2, 0, 0, 0, location), until.In(location))

	_, quiet = QuietUntil(time.Date(2025, 1, 7, 2, 0, 0, 0, location), setting)
	require.False(t, quiet)
}

// The window is evaluated in the configured zone, not the server's.
func TestQuietUntilUsesTheConfiguredTimezone(t *testing.T) {
	t.Parallel()

	setting := &storepb.QuietHoursSetting{
		Enabled:  true,
		Timezone: "UTC",
		Windows:  []*storepb.QuietWindow{recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "00:00", "01:00")},
	}
	now := time.Date(2025, 1, 6, 0, 30, 0, 0, time.UTC)
	until, quiet := QuietUntil(now, setting)
	require.True(t, quiet)
	require.Equal(t, time.Date(2025, 1, 6, 1, 0, 0, 0, time.UTC), until)
}

func TestQuietUntilMaintenanceWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 6, 2, 30, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	end := now.Add(90 * time.Minute)
	setting := &storepb.QuietHoursSetting{
		Enabled:            true,
		Timezone:           "UTC",
		MaintenanceWindows: []*storepb.MaintenanceWindow{{Start: timestamppb.New(start), End: timestamppb.New(end)}},
	}
	until, quiet := QuietUntil(now, setting)
	require.True(t, quiet)
	require.Equal(t, end, until)

	_, quiet = QuietUntil(end, setting)
	require.False(t, quiet, "the window is half-open")
}

// A missing, disabled or malformed schedule must fail open: a broken setting does
// not hold every alert.
func TestQuietUntilFailsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC)
	for name, setting := range map[string]*storepb.QuietHoursSetting{
		"nil":       nil,
		"disabled":  {Enabled: false, Timezone: "UTC", Windows: []*storepb.QuietWindow{recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "00:00", "23:59")}},
		"bad zone":  {Enabled: true, Timezone: "Mars/Olympus", Windows: []*storepb.QuietWindow{recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "00:00", "23:59")}},
		"bad clock": {Enabled: true, Timezone: "UTC", Windows: []*storepb.QuietWindow{recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "nope", "23:59")}},
	} {
		_, quiet := QuietUntil(now, setting)
		require.False(t, quiet, name)
	}
}

func TestValidateQuietHoursSetting(t *testing.T) {
	t.Parallel()

	valid := quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "22:00", "08:00"))
	require.NoError(t, ValidateQuietHoursSetting(valid))
	require.NoError(t, ValidateQuietHoursSetting(nil))
	require.NoError(t, ValidateQuietHoursSetting(&storepb.QuietHoursSetting{Enabled: false}))

	tests := map[string]*storepb.QuietHoursSetting{
		"no timezone":           {Enabled: true, Windows: valid.GetWindows()},
		"bad timezone":          {Enabled: true, Timezone: "Mars/Olympus", Windows: valid.GetWindows()},
		"no windows":            {Enabled: true, Timezone: "UTC"},
		"window without days":   quietSetting(recurringWindow(nil, "22:00", "08:00")),
		"invalid day":           quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek(9)}, "22:00", "08:00")),
		"invalid start":         quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "24:00", "08:00")),
		"same start and end":    quietSetting(recurringWindow([]storepb.DayOfWeek{storepb.DayOfWeek_DAY_OF_WEEK_MONDAY}, "22:00", "22:00")),
		"maintenance no end":    {Enabled: true, Timezone: "UTC", MaintenanceWindows: []*storepb.MaintenanceWindow{{Start: timestamppb.Now()}}},
		"maintenance backwards": {Enabled: true, Timezone: "UTC", MaintenanceWindows: []*storepb.MaintenanceWindow{{Start: timestamppb.New(time.Now().Add(time.Hour)), End: timestamppb.New(time.Now())}}},
	}
	for name, setting := range tests {
		require.Error(t, ValidateQuietHoursSetting(setting), name)
	}

	require.NoError(t, ValidateQuietHoursSetting(&storepb.QuietHoursSetting{
		Enabled:            true,
		Timezone:           "UTC",
		MaintenanceWindows: []*storepb.MaintenanceWindow{{Start: timestamppb.New(time.Now()), End: timestamppb.New(time.Now().Add(time.Hour))}},
	}))
}
