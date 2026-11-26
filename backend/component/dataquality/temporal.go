package dataquality

import (
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// temporalDateLayouts and temporalTimeOfDayLayouts are the ISO-8601 forms a rule
// may bind to a date/timestamp column and to a time-of-day column. The engines
// accept far more spellings than this, but the point of the whitelist is not to
// model every dialect: it is to turn "an expression that can never bind" into a
// save-time error, instead of an ERROR execution the author has to decode from a
// driver message. A rolling window is a freshness check or a time variable in a
// filter, never a bound.
var (
	temporalDateLayouts = []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.999999999",
		time.RFC3339,
		time.RFC3339Nano,
	}
	temporalTimeOfDayLayouts = []string{
		"15:04",
		"15:04:05",
		"15:04:05.999999999",
		"15:04:05Z07:00",
		"15:04:05.999999999Z07:00",
	}
)

// isTimeOfDayType reports whether the declared column type holds a time of day
// rather than a date or a timestamp. Both map to ColumnKindTemporal, so only the
// declared type can tell them apart.
func isTimeOfDayType(columnType string) bool {
	switch columnTypeBaseName(columnType) {
	case "time", "timetz":
		return true
	default:
		return false
	}
}

// parseTemporalLiteral reports whether value is one whole ISO-8601 literal of the
// column's temporal shape: a date or timestamp for a date/timestamp column, a time
// of day for a time column. A fragment such as "now - 1 day" is not.
func parseTemporalLiteral(column ColumnInfo, value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	layouts := temporalDateLayouts
	if isTimeOfDayType(column.Type) {
		layouts = temporalTimeOfDayLayouts
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, trimmed); err == nil {
			return true
		}
	}
	return false
}

// temporalLiteralError explains what the column expects and how to express the two
// things people actually mean: a fixed point in time, and a rolling window.
func temporalLiteralError(field string, column ColumnInfo, value string) error {
	shape := "date or timestamp"
	example := "2026-01-31 or 2026-01-31T00:00:00Z"
	if isTimeOfDayType(column.Type) {
		shape = "time of day"
		example = "18:30:00"
	}
	return common.Errorf(common.Invalid,
		"%s %q of column %q is not a %s; write an ISO-8601 literal (%s), or express a rolling window with a freshness check (max_lag_seconds) or a ${today-7d} filter value",
		field, value, column.Name, shape, example)
}
