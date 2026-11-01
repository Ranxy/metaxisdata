// Package schedule describes when a data-quality target comes due. A target runs
// on either a fixed interval or a standard five-field cron expression in an IANA
// time zone, never both, and the two forms share one rule: two runs may not be
// less than MinInterval apart.
//
// The package is a leaf so that both the store (which persists a target's
// schedule and seeds its next run) and the runner (which advances it) can use the
// same parsing and next-run math without depending on the data quality component.
package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	// MinInterval is the shortest time between two runs of a target. It bounds an
	// interval directly and a cron expression through the gap between two
	// consecutive occurrences.
	MinInterval = 15 * time.Minute
	// MaxInterval bounds an interval schedule's length. A cron schedule has no
	// upper bound: a monthly or yearly occurrence is an explicit intent.
	MaxInterval = 7 * 24 * time.Hour

	// cronGapProbe is how many cron occurrences the validator follows to verify
	// the minimum gap. Any expression denser than MinInterval shows a short gap
	// among the first two.
	cronGapProbe = 16
	// maxCronBytes bounds a cron expression. It is far above any real expression,
	// but a bounded input keeps a malformed request from being echoed back at
	// request-body size.
	maxCronBytes = 256
	// repeatedWallClockProbes bounds how many duplicated wall clocks Next may skip
	// before giving up. A fall-back repeats at most the widest fold (two hours in
	// Antarctica/Troll) at the finest cron granularity (one minute), so 120 is the
	// theoretical maximum; a valid schedule is limited to 15-minute spacing and
	// skips at most nine.
	repeatedWallClockProbes = 128
)

// repeatedWallClockDeltas are the offsets a daylight-saving fall-back can repeat a
// wall clock at. One hour covers nearly every zone; 30 minutes (Lord Howe) and the
// historical two-hour shifts are included so a stored schedule cannot fire twice.
var repeatedWallClockDeltas = [...]time.Duration{30 * time.Minute, time.Hour, 90 * time.Minute, 2 * time.Hour}

// Spec is a target's schedule: Interval for an interval target, Cron plus
// TimeZone for a cron target. The two forms are exclusive.
type Spec struct {
	Interval time.Duration
	Cron     string
	// TimeZone is the IANA zone a cron expression is evaluated in. Empty on an
	// interval target, and never empty on a cron target the API stored.
	TimeZone string
}

// IsCron reports whether the spec runs on a cron expression.
func (s Spec) IsCron() bool {
	return strings.TrimSpace(s.Cron) != ""
}

// Zone returns the location a cron expression is evaluated in. It never returns
// nil: an empty or unreadable name reads as UTC.
func (s Spec) Zone() *time.Location {
	if name := strings.TrimSpace(s.TimeZone); name != "" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
	}
	return time.UTC
}

// Parse validates the schedule a request names and resolves it. Exactly one of
// interval and cron must be set. An interval is MinInterval..MaxInterval; a cron
// expression is a standard five-field expression whose occurrences are at least
// MinInterval apart and which fires at all. A cron time zone is an IANA name, and
// an empty one resolves to defaultTimeZone (UTC when that is empty too), so the
// stored schedule is self-contained.
func Parse(interval time.Duration, expression, timeZone, defaultTimeZone string) (Spec, error) {
	expression = strings.TrimSpace(expression)
	if len(expression) > maxCronBytes {
		return Spec{}, fmt.Errorf("cron expression is longer than %d bytes", maxCronBytes)
	}
	switch {
	case interval > 0 && expression != "":
		return Spec{}, errors.New("a target schedule is either a schedule interval or a cron expression, not both")
	case interval > 0:
		if interval < MinInterval || interval > MaxInterval {
			return Spec{}, fmt.Errorf("schedule_interval %s is outside the accepted range %s–%s", interval, MinInterval, MaxInterval)
		}
		return Spec{Interval: interval}, nil
	case expression == "":
		return Spec{}, errors.New("a target schedule needs a schedule_interval or a cron expression")
	}

	if timeZone = strings.TrimSpace(timeZone); timeZone == "" {
		timeZone = strings.TrimSpace(defaultTimeZone)
	}
	if timeZone == "" {
		timeZone = "UTC"
	}
	if err := ValidateTimeZoneName(timeZone); err != nil {
		return Spec{}, err
	}
	spec := Spec{Cron: expression, TimeZone: timeZone}
	if err := spec.validateCron(time.Now()); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// ValidateTimeZoneName reports whether name is an IANA time zone a schedule may
// store: "UTC", or an Area/Location name such as "Asia/Shanghai". Fixed-offset
// abbreviations ("EST", "MST") and the process-local "Local" are refused: they are
// not IANA names, and a stored schedule must not depend on the deployment host's
// zone or silently mean a fixed offset.
func ValidateTimeZoneName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a time zone is required")
	}
	if name == "UTC" {
		return nil
	}
	if !strings.Contains(name, "/") {
		return fmt.Errorf("timezone %q is not an IANA time zone: use Area/Location, for example Asia/Shanghai", name)
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("timezone %q is not a valid IANA time zone", name)
	}
	return nil
}

// validateCron checks the expression's shape and its occurrence gap. The
// expression is a standard five-field form, so descriptors such as "@daily" are
// refused: they are another vocabulary and would not round-trip through the
// editor.
func (s Spec) validateCron(now time.Time) error {
	if len(strings.Fields(s.Cron)) != 5 {
		return fmt.Errorf("cron %q must be a standard five-field expression (minute hour day-of-month month day-of-week)", s.Cron)
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return fmt.Errorf("cron %q is not a valid expression: %v", s.Cron, err)
	}
	probe := now.In(s.Zone())
	var previous time.Time
	for range cronGapProbe {
		next := s.nextOccurrence(parsed, probe)
		if next.IsZero() {
			return fmt.Errorf("cron %q never fires: no occurrence within five years", s.Cron)
		}
		if !previous.IsZero() && next.Sub(previous) < MinInterval {
			return fmt.Errorf("cron %q runs more often than every %s", s.Cron, MinInterval)
		}
		previous = next
		probe = next
	}
	return nil
}

// nextOccurrence returns the first occurrence strictly after the given instant,
// skipping the second copy of a wall-clock time that a daylight-saving fall-back
// repeats: a schedule fires once per wall-clock occurrence, so a target must not
// run twice in the repeated hour. It returns the zero time when the expression has
// no further occurrence.
func (s Spec) nextOccurrence(parsed cron.Schedule, after time.Time) time.Time {
	zone := s.Zone()
	candidate := parsed.Next(after.In(zone))
	for range repeatedWallClockProbes {
		if candidate.IsZero() {
			return time.Time{}
		}
		if !isRepeatedWallClock(candidate, zone) {
			return candidate.UTC()
		}
		// This candidate is the second copy of a wall clock. Skip it one occurrence
		// at a time rather than jumping: a long fold (two hours in a zone such as
		// Antarctica/Troll) repeats an entire evening of wall clocks, and a jump wide
		// enough to leave it would also step over the first legitimate occurrence
		// after the fold (the wall clock the transition lands on).
		candidate = parsed.Next(candidate)
	}
	if candidate.IsZero() {
		return time.Time{}
	}
	return candidate.UTC()
}

// isRepeatedWallClock reports whether the candidate's local wall clock already
// happened at an earlier instant, which is exactly what a fall-back hour does.
func isRepeatedWallClock(candidate time.Time, zone *time.Location) bool {
	local := candidate.In(zone)
	for _, delta := range repeatedWallClockDeltas {
		earlier := candidate.Add(-delta).In(zone)
		if earlier.Year() == local.Year() && earlier.YearDay() == local.YearDay() &&
			earlier.Hour() == local.Hour() && earlier.Minute() == local.Minute() &&
			earlier.Second() == local.Second() {
			return true
		}
	}
	return false
}

// Next returns the first instant at which the target becomes due, given that the
// scheduler asks at after. An interval target runs one interval later, which is
// also why a late run never queues catch-up runs. A cron target runs at its next
// occurrence strictly after after.
//
// A stored row whose cron expression cannot be parsed falls back to its interval
// (or MinInterval when that is unusable too): scheduling metadata must never make
// a target due on every tick, and reads of a bad row must not fail.
func (s Spec) Next(after time.Time) time.Time {
	if !s.IsCron() {
		return after.Add(s.intervalFloor())
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return after.Add(s.intervalFloor())
	}
	next := s.nextOccurrence(parsed, after)
	if next.IsZero() {
		return after.Add(s.intervalFloor())
	}
	return next
}

// NominalInterval is the cadence a caller should assume where it needs a duration
// rather than an instant: the interval itself, or the gap between the next two
// cron occurrences. It never returns zero. A cron schedule with no second
// occurrence reports MaxInterval. The run cadence gate does not use it — it
// compares due slots — so a daylight-saving transition shifting one gap does not
// change what a cadence means.
func (s Spec) NominalInterval(after time.Time) time.Duration {
	if !s.IsCron() {
		return s.intervalFloor()
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return s.intervalFloor()
	}
	first := s.nextOccurrence(parsed, after)
	if first.IsZero() {
		return MaxInterval
	}
	second := s.nextOccurrence(parsed, first)
	if second.IsZero() {
		return MaxInterval
	}
	if gap := second.Sub(first); gap > 0 {
		return gap
	}
	return MaxInterval
}

// intervalFloor resolves the interval of a spec that has no usable one, the way
// the runner's own backstop does: a bad row must not become due on every tick.
func (s Spec) intervalFloor() time.Duration {
	switch {
	case s.Interval <= 0:
		return MinInterval
	case s.Interval < MinInterval:
		return MinInterval
	case s.Interval > MaxInterval:
		return MaxInterval
	default:
		return s.Interval
	}
}
