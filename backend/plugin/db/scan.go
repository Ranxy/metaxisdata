package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ScanRows reads a result set into a QueryResult, normalizing every value and
// applying the row and size limits of opts.
//
// Normalization turns the driver-specific representation into one of nil, int64,
// float64, bool, string or time.Time. A driver that returns []byte for a value
// (the MySQL-wire family returns []byte for nearly every column) has it decoded
// according to the column's database type: integer and floating-point columns
// become numbers, boolean columns become bools, date/time columns become
// time.Time, and everything else (including an unparseable value) stays a
// string. Other driver values are passed through unchanged.
//
// MaxRows stops before the row that would exceed the limit, so Truncated is set
// only when at least one more row existed. MaxBytes accumulates an estimate of
// the returned value payload (8 bytes for a number, 1 for a bool, the exact
// string length, a fixed width for a time) and drops the whole row that would
// exceed the limit rather than returning a partial row. Both limits are checked
// after a row was read but before it is returned; rows that already fit stay.
//
// ScanRows does not own rows: the caller closes them. It reports the driver's
// error unchanged so the upper layer can classify it.
func ScanRows(rows *sql.Rows, opts QueryOptions) (*QueryResult, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	convertBytes := make([]func([]byte) any, len(columnTypes))
	for i, columnType := range columnTypes {
		convertBytes[i] = newBytesConverter(columnType.DatabaseTypeName())
	}

	result := &QueryResult{Columns: columns, Rows: make([][]any, 0)}
	values := make([]any, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	bytesRead := 0
	for rows.Next() {
		if opts.MaxRows > 0 && len(result.Rows) == opts.MaxRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		row := make([]any, len(values))
		rowBytes := 0
		for i, value := range values {
			row[i] = normalizeValue(value, convertBytes[i])
			rowBytes += valueBytes(row[i])
		}
		if opts.MaxBytes > 0 && bytesRead+rowBytes > opts.MaxBytes {
			result.Truncated = true
			break
		}
		result.Rows = append(result.Rows, row)
		bytesRead += rowBytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// normalizeValue maps one scanned value to the normalized contract of
// QueryResult. database/sql yields only driver.Value types here, so the default
// branch is a safety net for a driver that returns something else.
func normalizeValue(value any, convertBytes func([]byte) any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []byte:
		return convertBytes(typed)
	case int64, float64, bool, string, time.Time:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// newBytesConverter returns the decoder for one column, chosen from the database
// type name the driver reports. The matching is deliberately loose: the type
// names differ between engines, a wrong guess only means the value is returned
// as a string instead of a parsed number, and no data is lost either way.
func newBytesConverter(databaseTypeName string) func([]byte) any {
	switch columnTypeKind(databaseTypeName) {
	case columnKindInteger:
		return func(raw []byte) any {
			if value, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
				return value
			}
			return string(raw)
		}
	case columnKindFloat:
		return func(raw []byte) any {
			if value, err := strconv.ParseFloat(string(raw), 64); err == nil {
				return value
			}
			return string(raw)
		}
	case columnKindBoolean:
		return parseBoolean
	case columnKindTime:
		return func(raw []byte) any {
			if value, ok := parseTime(string(raw)); ok {
				return value
			}
			return string(raw)
		}
	default:
		return func(raw []byte) any {
			return string(raw)
		}
	}
}

type columnKind int

const (
	columnKindString columnKind = iota
	columnKindInteger
	columnKindFloat
	columnKindBoolean
	columnKindTime
)

func columnTypeKind(databaseTypeName string) columnKind {
	name := strings.ToUpper(databaseTypeName)
	switch {
	case strings.Contains(name, "INT"), strings.Contains(name, "SERIAL"):
		// BIGINT, INT, SMALLINT, TINYINT, MEDIUMINT, INT4, INT8, SERIAL.
		return columnKindInteger
	case strings.Contains(name, "FLOAT"), strings.Contains(name, "DOUBLE"),
		strings.Contains(name, "REAL"), strings.Contains(name, "DECIMAL"),
		strings.Contains(name, "NUMERIC"), strings.Contains(name, "MONEY"):
		return columnKindFloat
	case name == "BOOL", name == "BOOLEAN", name == "BIT":
		return columnKindBoolean
	case strings.Contains(name, "TIMESTAMP"), strings.Contains(name, "DATETIME"),
		strings.Contains(name, "DATE"), strings.Contains(name, "TIME"):
		return columnKindTime
	default:
		return columnKindString
	}
}

// timeLayouts covers the textual time forms of the MySQL-wire engines, whose
// drivers return date/time values as []byte. PostgreSQL returns time.Time
// directly, so it never reaches this list.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"15:04:05.999999999",
	"15:04:05",
}

func parseTime(raw string) (time.Time, bool) {
	for _, layout := range timeLayouts {
		if value, err := time.Parse(layout, raw); err == nil {
			return value, true
		}
	}
	return time.Time{}, false
}

func parseBoolean(raw []byte) any {
	// MySQL BIT columns return the raw bytes, not the textual "0"/"1".
	if len(raw) == 1 && (raw[0] == 0 || raw[0] == 1) {
		return raw[0] == 1
	}
	if value, err := strconv.ParseBool(string(raw)); err == nil {
		return value
	}
	return string(raw)
}

// timeValueBytes is the fixed estimate used for a time.Time value: the length of
// its widest RFC 3339 form.
const timeValueBytes = len("2006-01-02T15:04:05.999999999Z07:00")

// valueBytes estimates the payload size of one normalized value for MaxBytes.
func valueBytes(value any) int {
	switch typed := value.(type) {
	case nil:
		return 0
	case int64, float64:
		return 8
	case bool:
		return 1
	case time.Time:
		return timeValueBytes
	case string:
		return len(typed)
	default:
		return len(fmt.Sprintf("%v", typed))
	}
}
