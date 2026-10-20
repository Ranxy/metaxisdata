package dataquality

import (
	"context"
	"fmt"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// ProfileOptions bounds one table profile. The zero value is the safe default in
// every field, so the runner can pass the workspace settings and leave unset
// values to the profiler.
type ProfileOptions struct {
	// MaxColumns is how many columns one profile may read. A table with more
	// columns is refused rather than silently truncated, so the caller's column
	// selection stays meaningful. 0 reads as 50.
	MaxColumns int
	// SampleRows is the row bound of the sample query. 0 reads as 1000. The
	// aggregate is always exact over the whole table.
	SampleRows int
	// QueryTimeout bounds each of the two queries. 0 reads as 120s.
	QueryTimeout time.Duration
	// MaxBytes bounds the accumulated value payload a query may return. 0 reads
	// as 4 MiB.
	MaxBytes int
}

// normalized applies the defaults to a ProfileOptions.
func (o ProfileOptions) normalized() ProfileOptions {
	if o.MaxColumns <= 0 {
		o.MaxColumns = defaultProfileMaxColumns
	}
	if o.SampleRows <= 0 {
		o.SampleRows = defaultProfileSampleRows
	}
	if o.QueryTimeout <= 0 {
		o.QueryTimeout = defaultProfileQueryTimeout
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = defaultProfileMaxBytes
	}
	return o
}

// TableProfile is one table's deterministic profile: a ColumnProfile per
// requested column, plus how the sample was taken. It is the input to the
// semantic labeler and to the review UI; it holds no sample rows, only the
// summaries computed from them.
type TableProfile struct {
	// Columns maps a column name to its profile. A column the caller named is
	// always present, even when the whole table is empty.
	Columns map[string]*storepb.ColumnProfile
	// SampleMethod describes how the sample was bounded. v1 always uses "limit".
	SampleMethod string
	// SampleRows is how many rows the sample actually returned.
	SampleRows int64
	// Truncated reports that a query was cut off by a row or byte limit, so the
	// aggregate or the sample is not the whole truth.
	Truncated bool
}

// Profiler computes deterministic column profiles. It holds no state and no
// connection: every call takes the read-only queryer the caller opened.
type Profiler struct{}

// NewProfiler returns a profiler.
func NewProfiler() *Profiler {
	return &Profiler{}
}

// Profile reads one table's column statistics with two read-only queries: one
// aggregate over the whole table (counts, distinct counts, extremes, string
// lengths) and one bounded sample (frequent values and value shapes for string
// columns). It never selects a whole row for storage: the sample stays in this
// process and only summaries leave it.
//
// A named column must exist on the target and have a classifiable type; a profile
// that asks for more than opts.MaxColumns is refused. A target engine without a
// data-quality dialect is refused instead of guessed at. The aggregate is the
// source of truth for counts, so a failed or empty sample only drops the
// sample-derived fields, and a failed sample query is an error rather than a
// silent partial profile.
func (*Profiler) Profile(ctx context.Context, queryer db.ReadOnlyQueryer, target ResolvedTarget, columns []string, opts ProfileOptions) (*TableProfile, error) {
	if queryer == nil {
		return nil, common.Errorf(common.Internal, "a read-only queryer is required to profile a table")
	}
	opts = opts.normalized()

	dialect, err := DialectFor(target.Engine)
	if err != nil {
		return nil, common.Errorf(common.Invalid, "%s", err)
	}
	selected, err := profileSelectedColumns(target, columns, opts.MaxColumns)
	if err != nil {
		return nil, err
	}
	table, err := compileTable(dialect, target)
	if err != nil {
		return nil, err
	}

	// The aggregate is exact over the whole table; it is the source of truth for
	// totals, null counts, distinct counts and extremes.
	aggregate, err := queryer.QueryReadOnly(ctx, buildProfileAggregateSQL(dialect, table, selected), nil, db.QueryOptions{
		MaxRows:  1,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
	})
	if err != nil {
		return nil, err
	}
	profiles, err := profilesFromAggregate(selected, aggregate)
	if err != nil {
		return nil, err
	}

	// The sample is bounded and only feeds the value-frequency and value-shape
	// summaries. A failed sample fails the profile: a profile that silently
	// omitted its patterns would look complete while being wrong.
	sample, err := queryer.QueryReadOnly(ctx, buildProfileSampleSQL(dialect, table, selected, opts.SampleRows), nil, db.QueryOptions{
		MaxRows:  opts.SampleRows,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
	})
	if err != nil {
		return nil, err
	}
	if err := applyProfileSample(profiles, selected, sample); err != nil {
		return nil, err
	}

	return &TableProfile{
		Columns:      profiles,
		SampleMethod: profileSampleMethod,
		SampleRows:   int64(len(sample.Rows)),
		Truncated:    aggregate.Truncated || sample.Truncated,
	}, nil
}

// profileSelectedColumns resolves the requested columns against the target,
// deduplicating them in request order. An empty request means "profile every
// column of the target", which is what the UI's "analyze this table" action
// sends. A duplicate is ignored rather than refused: it names the same column, so
// a second read of it would only cost SQL.
func profileSelectedColumns(target ResolvedTarget, columns []string, maxColumns int) ([]profileColumn, error) {
	names := columns
	if len(names) == 0 {
		names = target.ColumnNames()
	}
	if len(names) == 0 {
		return nil, invalidConfig("a column profile needs at least one column")
	}
	selected := make([]profileColumn, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		column, err := requireColumn(target, name)
		if err != nil {
			return nil, err
		}
		if column.Kind == ColumnKindUnknown {
			return nil, invalidConfig("column %q has a type this build cannot profile", column.Name)
		}
		capabilities := capabilitiesFor(target.Engine, column)
		if capabilities.noColumnCount && capabilities.noNullTest {
			return nil, invalidConfig("column %q has a type this build cannot profile", column.Name)
		}
		selected = append(selected, newProfileColumn(target.Engine, column))
	}
	if len(selected) > maxColumns {
		return nil, invalidConfig("a table profile allows at most %d columns, got %d", maxColumns, len(selected))
	}
	return selected, nil
}

// profilesFromAggregate maps the single aggregate row to one ColumnProfile per
// selected column. A missing column, a non-integral count or more non-NULL values
// than rows is BAD_DATA: reading any of them as zero would turn "the schema
// changed" into a clean-looking profile.
func profilesFromAggregate(selected []profileColumn, result *db.QueryResult) (map[string]*storepb.ColumnProfile, error) {
	aliases := profileAggregateAliases(selected)
	if result == nil {
		return nil, badData("the profile aggregate returned no result")
	}
	if len(result.Rows) != 1 {
		return nil, badData("the profile aggregate returned %d rows, expect 1", len(result.Rows))
	}
	indexes, err := profileResultIndexes(result.Columns, aliases)
	if err != nil {
		return nil, err
	}
	row := result.Rows[0]
	read := func(alias string) (any, error) {
		index := indexes[alias]
		if index >= len(row) {
			return nil, badData("the profile aggregate row has no value for %q", alias)
		}
		return row[index], nil
	}

	totalValue, err := read("total_rows")
	if err != nil {
		return nil, err
	}
	total, err := profileInt64(totalValue, "total_rows")
	if err != nil {
		return nil, err
	}
	if total < 0 {
		return nil, badData("total_rows is negative (%d)", total)
	}

	profiles := make(map[string]*storepb.ColumnProfile, len(selected))
	// The loop is unrolled per column rather than deferred so each read failure
	// names its own alias.
	for index, column := range selected {
		prefix := fmt.Sprintf("c%d_", index)
		profile, err := profileFromAggregateColumn(column, prefix, total, read)
		if err != nil {
			return nil, err
		}
		profiles[column.Name] = profile
	}
	return profiles, nil
}

// profileFromAggregateColumn reads one column's aggregate facts. It is separate
// from profilesFromAggregate so every alias read is explicit and a failure names
// the column it belongs to.
func profileFromAggregateColumn(column profileColumn, prefix string, total int64, read func(string) (any, error)) (*storepb.ColumnProfile, error) {
	nonNullValue, err := read(prefix + "non_null")
	if err != nil {
		return nil, err
	}
	nonNull, err := profileInt64(nonNullValue, prefix+"non_null")
	if err != nil {
		return nil, err
	}
	if nonNull < 0 || nonNull > total {
		return nil, badData("column %q reports %d non-NULL values in %d rows", column.Name, nonNull, total)
	}
	profile := &storepb.ColumnProfile{TotalRows: total, NullCount: total - nonNull}
	if column.Facts&profileFactDistinct != 0 {
		distinct, err := profileReadInt64(read, prefix+"distinct")
		if err != nil {
			return nil, err
		}
		profile.DistinctCount = distinct
	}
	if column.Facts&profileFactExtrema != 0 {
		minValue, err := read(prefix + "min")
		if err != nil {
			return nil, err
		}
		maxValue, err := read(prefix + "max")
		if err != nil {
			return nil, err
		}
		profile.MinValue = profileValueString(minValue)
		profile.MaxValue = profileValueString(maxValue)
	}
	if column.Facts&profileFactLength != 0 {
		minLengthValue, err := read(prefix + "min_len")
		if err != nil {
			return nil, err
		}
		maxLengthValue, err := read(prefix + "max_len")
		if err != nil {
			return nil, err
		}
		profile.MinLength, err = profileOptionalInt64(minLengthValue, prefix+"min_len")
		if err != nil {
			return nil, err
		}
		profile.MaxLength, err = profileOptionalInt64(maxLengthValue, prefix+"max_len")
		if err != nil {
			return nil, err
		}
	}
	return profile, nil
}

// applyProfileSample computes the sample-derived summaries: value shapes for
// columns whose values render as text, the estimated distinct count for types
// without COUNT(DISTINCT), and frequent values for low-cardinality columns. A
// sample with no rows leaves them unset rather than claiming an empty
// distribution.
func applyProfileSample(profiles map[string]*storepb.ColumnProfile, selected []profileColumn, result *db.QueryResult) error {
	if result == nil {
		return badData("the profile sample returned no result")
	}
	names := make([]string, 0, len(selected))
	for _, column := range selected {
		names = append(names, column.Name)
	}
	indexes, err := profileResultIndexes(result.Columns, names)
	if err != nil {
		return err
	}
	values := make([][]any, len(selected))
	for _, row := range result.Rows {
		for index, column := range selected {
			position := indexes[column.Name]
			if position >= len(row) {
				return badData("the profile sample row has no value for %q", column.Name)
			}
			values[index] = append(values[index], row[position])
		}
	}
	for index, column := range selected {
		profile := profiles[column.Name]
		if profile == nil {
			continue
		}
		if column.Patterns {
			profile.Patterns = profilePatterns(values[index])
		}
		if column.EstimatedDistinct {
			// The type has no COUNT(DISTINCT) support, so the sample is the only
			// reading available; it is exact when the sample saw every row.
			profile.DistinctCount = profileSampleDistinctCount(values[index])
			profile.DistinctEstimated = result.Truncated || int64(len(result.Rows)) < profile.TotalRows
		}
		// Top-K is only meaningful for a low-cardinality column; the cardinality
		// gate uses the exact aggregate count, the frequencies use the sample.
		if profile.DistinctCount > 0 && profile.DistinctCount <= profileTopValuesDistinctLimit {
			profile.TopValues = profileTopValues(values[index])
		}
	}
	return nil
}
