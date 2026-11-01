package schedule

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseInterval(t *testing.T) {
	spec, err := Parse(time.Hour, "", "", "")
	require.NoError(t, err)
	require.Equal(t, time.Hour, spec.Interval)
	require.False(t, spec.IsCron())

	// The bounds are inclusive, and the whole range is accepted.
	for _, interval := range []time.Duration{MinInterval, 7 * 24 * time.Hour, 90 * time.Minute} {
		_, err := Parse(interval, "", "", "")
		require.NoError(t, err, "interval %s", interval)
	}
	for _, interval := range []time.Duration{MinInterval - time.Second, MaxInterval + time.Second} {
		_, err := Parse(interval, "", "", "")
		require.Error(t, err, "interval %s", interval)
	}
}

func TestParseOneFormOnly(t *testing.T) {
	_, err := Parse(time.Hour, "0 2 * * *", "", "")
	require.ErrorContains(t, err, "not both")

	_, err = Parse(0, "", "", "")
	require.ErrorContains(t, err, "needs a schedule_interval or a cron expression")
}

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		cron    string
		zone    string
		defZone string
		wantErr string
	}{
		{name: "daily", cron: "0 2 * * *"},
		{name: "every fifteen minutes", cron: "*/15 * * * *"},
		{name: "weekly", cron: "0 2 * * 1"},
		{name: "monthly", cron: "0 3 1 * *"},
		{name: "yearly", cron: "0 3 1 1 *"},
		{name: "resolved default zone", cron: "0 2 * * *", defZone: "Asia/Shanghai"},
		{name: "explicit zone", cron: "0 2 * * *", zone: "America/New_York"},
		{name: "six fields", cron: "0 0 2 * * *", wantErr: "five-field"},
		{name: "four fields", cron: "0 2 * *", wantErr: "five-field"},
		{name: "descriptor", cron: "@daily", wantErr: "five-field"},
		{name: "garbage", cron: "nope * * * *", wantErr: "not a valid expression"},
		{name: "too frequent minutes", cron: "* * * * *", wantErr: "more often than"},
		{name: "too frequent steps", cron: "*/10 * * * *", wantErr: "more often than"},
		{name: "never fires", cron: "0 0 30 2 *", wantErr: "never fires"},
		{name: "bad zone", cron: "0 2 * * *", zone: "Mars/Base", wantErr: "not a valid IANA time zone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, err := Parse(0, test.cron, test.zone, test.defZone)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.cron, spec.Cron)
			require.True(t, spec.IsCron())
			require.NotEmpty(t, spec.TimeZone)
		})
	}
}

func TestParseCronResolvesZone(t *testing.T) {
	spec, err := Parse(0, "0 2 * * *", "", "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "Asia/Shanghai", spec.TimeZone)

	// The workspace default is the fallback, not an override: an explicit zone wins.
	spec, err = Parse(0, "0 2 * * *", "Europe/Berlin", "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, "Europe/Berlin", spec.TimeZone)

	// Without either, the schedule is stored in UTC rather than with an empty name.
	spec, err = Parse(0, "0 2 * * *", "", "")
	require.NoError(t, err)
	require.Equal(t, "UTC", spec.TimeZone)
}

func TestNextInterval(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, now.Add(90*time.Minute), Spec{Interval: 90 * time.Minute}.Next(now))

	// A malformed row cannot become due on every tick.
	require.Equal(t, now.Add(MinInterval), Spec{}.Next(now))
	require.Equal(t, now.Add(MinInterval), Spec{Interval: time.Minute}.Next(now))
	require.Equal(t, now.Add(MaxInterval), Spec{Interval: 30 * 24 * time.Hour}.Next(now))
}

func TestNextCron(t *testing.T) {
	// A daily 02:00 schedule in Shanghai: 02:00 local is 18:00 UTC the day before.
	spec := Spec{Cron: "0 2 * * *", TimeZone: "Asia/Shanghai"}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got := spec.Next(now)
	require.Equal(t, time.Date(2025, 1, 1, 18, 0, 0, 0, time.UTC), got)

	// Strictly after: asking at the exact occurrence returns the next day's.
	require.Equal(t, time.Date(2025, 1, 2, 18, 0, 0, 0, time.UTC), spec.Next(got))

	// A stored malformed expression falls back to the interval, not to every tick.
	broken := Spec{Interval: 2 * time.Hour, Cron: "0 2 * * * *"}
	require.Equal(t, now.Add(2*time.Hour), broken.Next(now))
}

func TestNextCronAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	spec := Spec{Cron: "0 2 * * *", TimeZone: "America/New_York"}

	// Spring forward: 02:00 does not exist on 2025-03-09, so that day's run is
	// skipped and the next occurrence is 2025-03-10 02:00 EDT.
	beforeSpring := time.Date(2025, 3, 8, 12, 0, 0, 0, location)
	next := spec.Next(beforeSpring)
	local := next.In(location)
	require.Equal(t, 2025, local.Year())
	require.Equal(t, time.March, local.Month())
	require.Equal(t, 10, local.Day())
	require.Equal(t, 2, local.Hour())

	// Fall back: 02:00 happens twice on 2025-11-02, and the schedule fires once.
	beforeFall := time.Date(2025, 11, 1, 12, 0, 0, 0, location)
	first := spec.Next(beforeFall)
	require.Equal(t, 2, first.In(location).Hour())
	require.Equal(t, 2, first.In(location).Day())
	second := spec.Next(first)
	require.Equal(t, 3, second.In(location).Day(), "the repeated 02:00 must fire once")
}

func TestNominalInterval(t *testing.T) {
	now := time.Date(2025, 1, 1, 8, 30, 0, 0, time.UTC)

	require.Equal(t, 6*time.Hour, Spec{Interval: 6 * time.Hour}.NominalInterval(now))
	require.Equal(t, 24*time.Hour, Spec{Cron: "0 2 * * *", TimeZone: "UTC"}.NominalInterval(now))
	require.Equal(t, 7*24*time.Hour, Spec{Cron: "0 2 * * 1", TimeZone: "UTC"}.NominalInterval(now))
	// A malformed cron keeps the interval fallback it was stored with.
	require.Equal(t, 2*time.Hour, Spec{Interval: 2 * time.Hour, Cron: "bogus"}.NominalInterval(now))
	// A schedule with no second occurrence reports the maximum.
	require.Equal(t, MaxInterval, Spec{Cron: "0 0 30 2 *", TimeZone: "UTC"}.NominalInterval(now))
}

// A wall-clock time that a fall-back repeats must fire once. robfig returns both
// copies (01:30 EDT and 01:30 EST); the schedule must skip the second.
func TestNextCronFallBackFiresOnce(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	spec := Spec{Cron: "30 1 * * *", TimeZone: "America/New_York"}

	before := time.Date(2025, time.November, 1, 12, 0, 0, 0, location)
	first := spec.Next(before)
	require.Equal(t, time.Date(2025, time.November, 2, 5, 30, 0, 0, time.UTC), first,
		"the first 01:30 is the EDT copy")
	local := first.In(location)
	require.Equal(t, 2, local.Day())
	require.Equal(t, 1, local.Hour())
	require.Equal(t, 30, local.Minute())

	second := spec.Next(first)
	require.Equal(t, time.Date(2025, time.November, 3, 6, 30, 0, 0, time.UTC), second,
		"the repeated 01:30 EST copy on the same day must be skipped")
	require.Equal(t, 3, second.In(location).Day())

	// The schedule's nominal cadence stays about a day; the repeated pair must not
	// look like an hourly expression.
	require.GreaterOrEqual(t, spec.NominalInterval(before), 24*time.Hour)
}

// Every occurrence inside a repeated hour fires once: the second pass over the
// same wall clocks is skipped, and the schedule resumes the next day.
func TestNextCronRepeatedHourFiresOncePerWallClock(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	spec := Spec{Cron: "*/15 1 * * *", TimeZone: "America/New_York"}

	// Start before the first 01:00 EDT; collect every occurrence in the repeated
	// hour (four wall clocks, each fired once).
	probe := time.Date(2025, time.November, 2, 0, 30, 0, 0, location)
	seen := make([]time.Time, 0, 4)
	for range 4 {
		next := spec.Next(probe)
		seen = append(seen, next)
		probe = next
	}
	for i, occurrence := range seen {
		local := occurrence.In(location)
		require.Equal(t, 2, local.Day(), "step %d is still on the fall-back day", i)
		require.Equal(t, 1, local.Hour())
	}
	require.Equal(t, []int{0, 15, 30, 45}, []int{
		seen[0].In(location).Minute(), seen[1].In(location).Minute(),
		seen[2].In(location).Minute(), seen[3].In(location).Minute(),
	})
	// The next occurrence after the first pass is the next day, not the repeated
	// second pass over 01:00-01:45 EST.
	next := spec.Next(seen[3])
	require.Equal(t, 3, next.In(location).Day())
	require.Equal(t, 1, next.In(location).Hour())
	require.Equal(t, 0, next.In(location).Minute())
}

func TestValidateTimeZoneName(t *testing.T) {
	for _, name := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/Berlin"} {
		require.NoError(t, ValidateTimeZoneName(name), name)
	}
	for _, name := range []string{"", "Local", "EST", "MST", "GMT", "Not/AZone"} {
		require.Error(t, ValidateTimeZoneName(name), name)
	}
}

func TestCronLengthIsBounded(t *testing.T) {
	_, err := Parse(0, "0 2 * * * "+strings.Repeat("x", 500), "UTC", "")
	require.ErrorContains(t, err, "longer than 256 bytes")
}

// A two-hour fold (Antarctica/Troll, the only such zone today) repeats a whole
// evening of wall clocks; the schedule must still fire once per wall clock rather
// than leak the second pass after a few skips.
func TestNextCronTwoHourFoldFiresOncePerWallClock(t *testing.T) {
	location, err := time.LoadLocation("Antarctica/Troll")
	require.NoError(t, err)
	spec := Spec{Cron: "*/15 * * * *", TimeZone: "Antarctica/Troll"}

	probe := time.Date(2025, time.October, 25, 22, 0, 0, 0, time.UTC)
	seen := map[string]struct{}{}
	fires := make([]time.Time, 0, 12)
	for range 12 {
		next := spec.Next(probe)
		require.False(t, next.IsZero())
		key := next.In(location).Format("2006-01-02 15:04")
		_, duplicate := seen[key]
		require.False(t, duplicate, "local wall clock %s fired twice", key)
		seen[key] = struct{}{}
		fires = append(fires, next)
		probe = next
	}

	// From 00:15 local, 11 fifteen-minute occurrences reach 02:45 local on the first
	// pass (indices 0..10); the second pass over 01:00..02:45 is skipped, so the next
	// one is the first wall clock after the fold: 03:00 local. (Skipping one occurrence
	// at a time, rather than jumping past the fold, is what keeps that boundary run.)
	require.Equal(t, 11, len(fires)-1)
	require.Equal(t, time.Date(2025, time.October, 26, 3, 0, 0, 0, time.UTC), fires[11])
	require.Equal(t, 3, fires[11].In(location).Hour())
	require.Equal(t, 0, fires[11].In(location).Minute())
}
