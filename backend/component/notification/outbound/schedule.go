package outbound

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// maxQuietWindows bounds the recurring schedule one workspace may store.
	maxQuietWindows = 20
	// maxMaintenanceWindows bounds the one-off schedule one workspace may store.
	maxMaintenanceWindows = 20
)

// ValidateQuietHoursSetting checks a quiet-hours schedule. A disabled schedule is
// accepted as it is: turning quiet hours off must not require cleaning the form.
func ValidateQuietHoursSetting(setting *storepb.QuietHoursSetting) error {
	if setting == nil || !setting.GetEnabled() {
		return nil
	}
	timezone := strings.TrimSpace(setting.GetTimezone())
	if timezone == "" {
		return errors.New("timezone is required when quiet hours are enabled")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return errors.New("timezone is not a valid IANA time zone")
	}
	if len(setting.GetWindows())+len(setting.GetMaintenanceWindows()) == 0 {
		return errors.New("at least one quiet or maintenance window is required")
	}
	if len(setting.GetWindows()) > maxQuietWindows {
		return fmt.Errorf("the schedule has more than %d quiet windows", maxQuietWindows)
	}
	if len(setting.GetMaintenanceWindows()) > maxMaintenanceWindows {
		return fmt.Errorf("the schedule has more than %d maintenance windows", maxMaintenanceWindows)
	}
	for _, window := range setting.GetWindows() {
		if len(window.GetDays()) == 0 {
			return errors.New("a quiet window needs at least one day")
		}
		for _, day := range window.GetDays() {
			if _, ok := isoWeekday(day); !ok {
				return errors.New("a quiet window names an invalid day")
			}
		}
		start, err := parseClock(window.GetStart())
		if err != nil {
			return fmt.Errorf("quiet window start %w", err)
		}
		end, err := parseClock(window.GetEnd())
		if err != nil {
			return fmt.Errorf("quiet window end %w", err)
		}
		if start == end {
			return errors.New("a quiet window start and end must differ")
		}
	}
	for _, window := range setting.GetMaintenanceWindows() {
		if window.GetStart() == nil || window.GetEnd() == nil {
			return errors.New("a maintenance window needs a start and an end")
		}
		if !window.GetStart().AsTime().Before(window.GetEnd().AsTime()) {
			return errors.New("a maintenance window must start before it ends")
		}
	}
	return nil
}

// QuietUntil reports whether now falls inside an active quiet window and, when it
// does, the instant the last active window ends. It is a pure function of the
// schedule and the clock. It fails open: a missing or unreadable schedule never
// blocks a delivery.
func QuietUntil(now time.Time, setting *storepb.QuietHoursSetting) (time.Time, bool) {
	if setting == nil || !setting.GetEnabled() {
		return time.Time{}, false
	}
	location, err := time.LoadLocation(strings.TrimSpace(setting.GetTimezone()))
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(location)
	var until time.Time
	quiet := false
	for _, window := range setting.GetWindows() {
		end, active := quietWindowEnd(local, window)
		if !active {
			continue
		}
		quiet = true
		if end.After(until) {
			until = end
		}
	}
	for _, window := range setting.GetMaintenanceWindows() {
		if window.GetStart() == nil || window.GetEnd() == nil {
			continue
		}
		start, end := window.GetStart().AsTime(), window.GetEnd().AsTime()
		if now.Before(start) || !now.Before(end) {
			continue
		}
		quiet = true
		if end.After(until) {
			until = end
		}
	}
	return until, quiet
}

// quietWindowEnd reports whether local falls inside one recurring window and, if
// it does, when that window ends. The window is checked for the day it starts on
// and for the previous day, because a window whose end is at or before its start
// crosses midnight.
func quietWindowEnd(local time.Time, window *storepb.QuietWindow) (time.Time, bool) {
	start, err := parseClock(window.GetStart())
	if err != nil {
		return time.Time{}, false
	}
	end, err := parseClock(window.GetEnd())
	if err != nil {
		return time.Time{}, false
	}
	days := make(map[time.Weekday]bool, len(window.GetDays()))
	for _, day := range window.GetDays() {
		if weekday, ok := isoWeekday(day); ok {
			days[weekday] = true
		}
	}
	for offset := -1; offset <= 0; offset++ {
		date := local.AddDate(0, 0, offset)
		if !days[date.Weekday()] {
			continue
		}
		windowStart := time.Date(date.Year(), date.Month(), date.Day(), start/60, start%60, 0, 0, local.Location())
		windowEnd := time.Date(date.Year(), date.Month(), date.Day(), end/60, end%60, 0, 0, local.Location())
		if !windowEnd.After(windowStart) {
			windowEnd = windowEnd.AddDate(0, 0, 1)
		}
		if !local.Before(windowStart) && local.Before(windowEnd) {
			return windowEnd, true
		}
	}
	return time.Time{}, false
}

// parseClock reads "HH:MM" as minutes since midnight.
func parseClock(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, errors.New(`must be "HH:MM"`)
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, errors.New(`must be "HH:MM"`)
	}
	return hour*60 + minute, nil
}

// isoWeekday maps the ISO 8601 weekday of the proto (Monday 1 .. Sunday 7) to the
// standard library's weekday.
func isoWeekday(day storepb.DayOfWeek) (time.Weekday, bool) {
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
