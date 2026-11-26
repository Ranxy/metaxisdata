package dataquality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rule that motivated this: a range bound of "now - 1 day" saved and was
// adopted automatically, then failed every run with SQLSTATE 22007, and the agent
// that owned it could not tell why. A bound that can never bind must be a save-time
// error naming the supported shapes.
func TestValidateRangeBoundRejectsAnExpression(t *testing.T) {
	t.Parallel()

	timestamp := ColumnInfo{Name: "performed_at", Type: "timestamptz", Kind: ColumnKindTemporal}
	err := validateRangeBound(timestamp, "min", "now - 1 day")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a date or timestamp")
	assert.Contains(t, err.Error(), "freshness")
	assert.Contains(t, err.Error(), "${today-7d}")

	for _, value := range []string{
		"2026-01-31",
		"2026-01-31 18:30:00",
		"2026-01-31T18:30:00Z",
		"2026-01-31T18:30:00.123456789+08:00",
	} {
		assert.NoError(t, validateRangeBound(timestamp, "min", value), value)
	}
}

// A time column and a date column both map to ColumnKindTemporal; only the
// declared type tells them apart, and a time-of-day bound is not a date.
func TestValidateRangeBoundUsesTheDeclaredTemporalShape(t *testing.T) {
	t.Parallel()

	timeOfDay := ColumnInfo{Name: "shift_start", Type: "time", Kind: ColumnKindTemporal}
	assert.NoError(t, validateRangeBound(timeOfDay, "min", "18:30:00"))
	assert.NoError(t, validateRangeBound(timeOfDay, "min", "18:30"))
	assert.Error(t, validateRangeBound(timeOfDay, "min", "2026-01-31"))

	date := ColumnInfo{Name: "performed_on", Type: "date", Kind: ColumnKindTemporal}
	assert.NoError(t, validateRangeBound(date, "min", "2026-01-31"))
	assert.Error(t, validateRangeBound(date, "min", "18:30:00"))
}

// A filter value is bound the same way a bound is, so a free-text expression there
// fails just as late. A time variable is resolved before a filter is compiled and
// stays untouched here.
func TestConvertFilterValueChecksATemporalLiteral(t *testing.T) {
	t.Parallel()

	timestamp := ColumnInfo{Name: "performed_at", Type: "timestamptz", Kind: ColumnKindTemporal}
	_, err := convertFilterValue(timestamp, "now - 1 day")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filter value")

	value, err := convertFilterValue(timestamp, "2026-01-31 00:00:00")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-31 00:00:00", value)

	value, err = convertFilterValue(timestamp, "${today-7d}")
	require.NoError(t, err)
	assert.Equal(t, "${today-7d}", value)
}
