package schedule

import (
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
