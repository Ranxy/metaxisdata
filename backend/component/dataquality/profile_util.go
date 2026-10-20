package dataquality

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Profiling limits. They mirror the design's cost bound (a single table profiles
// at most 50 columns with at most two queries) and the storage budget for the
// summaries (top values are truncated and capped).
const (
	// defaultProfileMaxColumns bounds one profile when the caller names no limit.
	defaultProfileMaxColumns = 50
	// defaultProfileSampleRows is the sample size when the caller names none.
	defaultProfileSampleRows = 1000
	// defaultProfileQueryTimeout bounds each of the two profile queries.
	defaultProfileQueryTimeout = 120 * time.Second
	// defaultProfileMaxBytes bounds the value payload the queries may return.
	defaultProfileMaxBytes = 4 << 20
	// profileTopValuesDistinctLimit is the largest distinct count for which the
	// sample's frequent values are worth listing; a wider column is an identifier
	// or free text, not an enumeration.
	profileTopValuesDistinctLimit = 20
	// profileMaxTopValues is how many frequent values one profile keeps.
	profileMaxTopValues = 10
	// profileMaxValueBytes bounds one display value, so a wide text column cannot
	// bloat the profile row.
	profileMaxValueBytes = 64
	// profileSampleMethod is the sample-method label v1 records on every profile.
	profileSampleMethod = "limit"
)

// profileFacts is the set of aggregate facts a column's type supports. The rule
// compiler's ColumnKind is about how a bound literal is read, not about what an
// aggregate may compute: PostgreSQL and SQL Server have neither MIN/MAX nor a
// length function for jsonb, xml, spatial or binary types, and SQL Server cannot
// even COUNT a legacy text column. The profiler only emits a fact the type table in
// capabilities.go claims — emitting one more fails the whole statement, not just
// that fact.
type profileFacts uint8

const (
	// profileFactDistinct is an exact COUNT(DISTINCT col).
	profileFactDistinct profileFacts = 1 << iota
	// profileFactExtrema is MIN/MAX on the column.
	profileFactExtrema
	// profileFactLength is MIN/MAX of the character length of the column.
	profileFactLength
)

// profileColumn is one column a profile reads: its registry name and the facts its
// type supports. EstimatedDistinct marks a type without COUNT(DISTINCT) support,
// whose distinct count comes from the sample instead; Patterns marks a column whose
// sample values render as human text.
type profileColumn struct {
	Name              string
	Facts             profileFacts
	EstimatedDistinct bool
	Patterns          bool
}

// newProfileColumn derives one column's profile facts from the shared type
// capability table, so the profile and the rule validators cannot drift apart.
func newProfileColumn(engine storepb.Engine, column ColumnInfo) profileColumn {
	capabilities := capabilitiesFor(engine, column)
	profile := profileColumn{
		Name:              column.Name,
		EstimatedDistinct: !capabilities.countDistinct,
		Patterns:          capabilities.text,
	}
	if capabilities.countDistinct {
		profile.Facts |= profileFactDistinct
	}
	if capabilities.extrema {
		profile.Facts |= profileFactExtrema
	}
	if capabilities.length {
		profile.Facts |= profileFactLength
	}
	return profile
}

// profileAggregateAliases is the ordered result shape of the aggregate query:
// total_rows followed by each column's aliases, in the same order the SQL lists
// them. Column aliases are positional ASCII ("c0_", "c1_", ...) so a column name
// with quotes or non-ASCII characters can never break the statement or exceed an
// engine's identifier limit.
func profileAggregateAliases(columns []profileColumn) []string {
	aliases := make([]string, 0, 1+len(columns)*6)
	aliases = append(aliases, "total_rows")
	for index, column := range columns {
		prefix := profileAliasPrefix(index)
		aliases = append(aliases, prefix+"non_null")
		if column.Facts&profileFactDistinct != 0 {
			aliases = append(aliases, prefix+"distinct")
		}
		if column.Facts&profileFactExtrema != 0 {
			aliases = append(aliases, prefix+"min", prefix+"max")
		}
		if column.Facts&profileFactLength != 0 {
			aliases = append(aliases, prefix+"min_len", prefix+"max_len")
		}
	}
	return aliases
}

// profileAliasPrefix is the positional alias prefix of one column.
func profileAliasPrefix(index int) string {
	return "c" + strconv.Itoa(index) + "_"
}

// buildProfileAggregateSQL renders the single aggregate statement. It reads the
// whole table (a profile is not filtered) with one scan: the row count, then per
// column the non-NULL count and the facts its type supports.
//
// A column with no aggregate fact gets its non-NULL count from a CASE expression
// rather than COUNT(col): SQL Server rejects COUNT on text, ntext and image, and the
// CASE form is valid wherever IS NOT NULL is.
func buildProfileAggregateSQL(dialect Dialect, table string, columns []profileColumn) string {
	expressions := make([]string, 0, 1+len(columns)*6)
	expressions = append(expressions, "COUNT(*) AS total_rows")
	for index, column := range columns {
		prefix := profileAliasPrefix(index)
		quoted := qualify(dialect, column.Name)
		if column.Facts == 0 {
			expressions = append(expressions,
				"COUNT(CASE WHEN "+quoted+" IS NOT NULL THEN 1 END) AS "+prefix+"non_null")
			continue
		}
		expressions = append(expressions, "COUNT("+quoted+") AS "+prefix+"non_null")
		if column.Facts&profileFactDistinct != 0 {
			expressions = append(expressions, "COUNT(DISTINCT "+quoted+") AS "+prefix+"distinct")
		}
		if column.Facts&profileFactExtrema != 0 {
			expressions = append(expressions,
				"MIN("+quoted+") AS "+prefix+"min",
				"MAX("+quoted+") AS "+prefix+"max")
		}
		if column.Facts&profileFactLength != 0 {
			length := dialect.LengthExpr(quoted)
			expressions = append(expressions,
				"MIN("+length+") AS "+prefix+"min_len",
				"MAX("+length+") AS "+prefix+"max_len")
		}
	}
	return "SELECT " + strings.Join(expressions, ", ") + " FROM " + table
}

// buildProfileSampleSQL renders the bounded sample statement. It selects only the
// profiled columns, never the whole row. T-SQL only accepts OFFSET/FETCH after an
// ORDER BY and the sample needs no natural order, so a constant ordering is
// emitted there; the other engines take a plain LIMIT.
func buildProfileSampleSQL(dialect Dialect, table string, columns []profileColumn, limit int) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, qualify(dialect, column.Name))
	}
	query := "SELECT " + strings.Join(quoted, ", ") + " FROM " + table
	if _, ok := dialect.(mssqlDialect); ok {
		query += " ORDER BY (SELECT NULL)"
	}
	return query + " " + dialect.LimitClause(limit)
}

// profileResultIndexes maps the required result column names to their positions.
// A missing column is BAD_DATA, never a silent zero: a profile that read the
// wrong column would state facts about the wrong data.
func profileResultIndexes(columns, expected []string) (map[string]int, error) {
	indexes := make(map[string]int, len(columns))
	for index, name := range columns {
		if _, ok := indexes[name]; !ok {
			indexes[name] = index
		}
	}
	for _, name := range expected {
		if _, ok := indexes[name]; !ok {
			return nil, badData("the profile query result has no %q column", name)
		}
	}
	return indexes, nil
}

// profileValueString renders one observed value for display, truncated to the
// storage bound. A NULL renders as the empty string: "no value" and "empty text"
// both display as nothing, and the null count already carries the distinction.
func profileValueString(value any) string {
	return common.TruncateUTF8Bytes(profileValueText(value), profileMaxValueBytes)
}

// profileValueText renders one observed value without truncation. It is the full
// rendering a sample's distinct count keys on: two values that differ only past
// the display bound are still two distinct values.
func profileValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// profileSampleDistinctCount counts the distinct values a sample saw. It is the
// fallback for a column whose type has no SQL equality for an exact COUNT
// (PostgreSQL json and xml, spatial types): the sample is the only reading the
// profile can take without an engine-specific cast.
func profileSampleDistinctCount(values []any) int64 {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		seen[profileValueText(value)] = struct{}{}
	}
	return int64(len(seen))
}

// profileReadInt64 reads one integer aggregate alias through the row reader.
func profileReadInt64(read func(string) (any, error), alias string) (int64, error) {
	value, err := read(alias)
	if err != nil {
		return 0, err
	}
	return profileInt64(value, alias)
}

// profileInt64 reads one integer aggregate. The normalized row carries int64; a
// driver that reports a numeric as float64 or a loose numeric string still maps,
// but a non-integral number is a contract violation rather than a rounded count.
func profileInt64(value any, column string) (int64, error) {
	switch typed := value.(type) {
	case nil:
		// An aggregate over an empty set is NULL; for a count that is zero.
		return 0, nil
	case int64:
		return typed, nil
	case float64:
		if typed != math.Trunc(typed) {
			return 0, badData("column %q is not an integer (%v)", column, typed)
		}
		return int64(typed), nil
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed, nil
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil || parsed != math.Trunc(parsed) {
			return 0, badData("column %q is not an integer (%q)", column, typed)
		}
		return int64(parsed), nil
	default:
		return 0, badData("column %q has an unexpected %T value", column, value)
	}
}

// profileOptionalInt64 reads one nullable integer (the text length bounds). A NULL
// stays nil: it means the column held no non-NULL value, which is different from
// a length of zero.
func profileOptionalInt64(value any, column string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := profileInt64(value, column)
	if err != nil {
		return nil, err
	}
	length := parsed
	return &length, nil
}

// profilePatterns summarizes the shapes of a string sample. One shape yields that
// shape; several yield "mixed"; a sample without a single non-empty value yields
// nothing, so the UI can say "no values seen" instead of "unknown".
func profilePatterns(values []any) []string {
	shapes := make(map[string]struct{}, 3)
	for _, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(profileValueString(value))
		if text == "" {
			continue
		}
		shapes[profileValueShape(text)] = struct{}{}
	}
	switch len(shapes) {
	case 0:
		return nil
	case 1:
		for shape := range shapes {
			return []string{shape}
		}
		return nil
	default:
		return []string{"mixed"}
	}
}

// profileValueShape classifies one non-empty text value. The order matters:
// numbers are checked before dates because a date is not numeric, and dates
// before text because a date-like value contains digits and separators.
func profileValueShape(text string) string {
	if _, err := strconv.ParseFloat(text, 64); err == nil {
		return "numeric"
	}
	if profileLooksLikeDate(text) {
		return "date-like"
	}
	if profileLooksLikeAlpha(text) {
		return "alpha"
	}
	return "other"
}

// profileDateLayouts are the spellings a sampled date/time value can arrive in.
var profileDateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// profileLooksLikeDate reports whether text parses as one of the date spellings.
func profileLooksLikeDate(text string) bool {
	for _, layout := range profileDateLayouts {
		if _, err := time.Parse(layout, text); err == nil {
			return true
		}
	}
	return false
}

// profileLooksLikeAlpha reports whether text is made of letters, spaces and
// common word punctuation, which is what a human-readable name looks like.
func profileLooksLikeAlpha(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || r == '-' || r == '\'' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// profileTopValues returns the sample's most frequent values, at most ten, ordered
// by descending frequency and then by value so the profile is deterministic. Each
// value is truncated before counting, so two long values that differ only past the
// display bound merge into one entry instead of producing two indistinguishable
// rows. The counts are sample frequencies, not table frequencies.
func profileTopValues(values []any) []*storepb.ProfileValue {
	counts := make(map[string]int64, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		text := profileValueString(value)
		if text == "" {
			continue
		}
		counts[text]++
	}
	if len(counts) == 0 {
		return nil
	}

	type countedValue struct {
		value string
		count int64
	}
	entries := make([]countedValue, 0, len(counts))
	for value, count := range counts {
		entries = append(entries, countedValue{value: value, count: count})
	}
	slices.SortFunc(entries, func(left, right countedValue) int {
		if left.count != right.count {
			return cmp.Compare(right.count, left.count)
		}
		return strings.Compare(left.value, right.value)
	})
	if len(entries) > profileMaxTopValues {
		entries = entries[:profileMaxTopValues]
	}

	result := make([]*storepb.ProfileValue, 0, len(entries))
	for _, entry := range entries {
		result = append(result, &storepb.ProfileValue{Value: entry.value, Count: entry.count})
	}
	return result
}
