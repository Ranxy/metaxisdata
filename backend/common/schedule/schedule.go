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
)

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
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return Spec{}, fmt.Errorf("timezone %q is not a valid IANA time zone", timeZone)
	}
	if err := validateCron(expression, location, time.Now()); err != nil {
		return Spec{}, err
	}
	return Spec{Cron: expression, TimeZone: timeZone}, nil
}

// validateCron checks the expression's shape and its occurrence gap. The
// expression is a standard five-field form, so descriptors such as "@daily" are
// refused: they are another vocabulary and would not round-trip through the
// editor.
func validateCron(expression string, location *time.Location, now time.Time) error {
	if len(strings.Fields(expression)) != 5 {
		return fmt.Errorf("cron %q must be a standard five-field expression (minute hour day-of-month month day-of-week)", expression)
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return fmt.Errorf("cron %q is not a valid expression: %v", expression, err)
	}
	probe := now.In(location)
	var previous time.Time
	for range cronGapProbe {
		next := parsed.Next(probe)
		if next.IsZero() {
			return fmt.Errorf("cron %q never fires: no occurrence within five years", expression)
		}
		if !previous.IsZero() && next.Sub(previous) < MinInterval {
			return fmt.Errorf("cron %q runs more often than every %s", expression, MinInterval)
		}
		previous = next
		probe = next
	}
	return nil
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
	next := parsed.Next(after.In(s.Zone()))
	if next.IsZero() {
		return after.Add(s.intervalFloor())
	}
	return next.UTC()
}

// NominalInterval is the cadence a caller should assume where it needs a duration
// rather than an instant: the interval itself, or the gap between the next two
// cron occurrences. It never returns zero. A cron schedule with no second
// occurrence reports MaxInterval.
func (s Spec) NominalInterval(after time.Time) time.Duration {
	if !s.IsCron() {
		return s.intervalFloor()
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return s.intervalFloor()
	}
	first := parsed.Next(after.In(s.Zone()))
	if first.IsZero() {
		return MaxInterval
	}
	second := parsed.Next(first)
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
