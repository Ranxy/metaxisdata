package dataquality

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// profileFakeQueryer serves the two queries a profile issues: the aggregate first,
// then the sample. It records the exact SQL and options so the goldens can pin
// them without a database.
type profileFakeQueryer struct {
	aggregate *db.QueryResult
	sample    *db.QueryResult

	aggregateErr error
	sampleErr    error

	aggregateSQL  string
	sampleSQL     string
	aggregateOpts db.QueryOptions
	sampleOpts    db.QueryOptions
	calls         int
}

func (f *profileFakeQueryer) QueryReadOnly(_ context.Context, query string, _ []any, opts db.QueryOptions) (*db.QueryResult, error) {
	f.calls++
	switch f.calls {
	case 1:
		f.aggregateSQL, f.aggregateOpts = query, opts
		return f.aggregate, f.aggregateErr
	case 2:
		f.sampleSQL, f.sampleOpts = query, opts
		return f.sample, f.sampleErr
	default:
		return nil, fmt.Errorf("unexpected profile query %d: %s", f.calls, query)
	}
}

// profileTestColumns resolves the profiled columns through the production path so
// the tests exercise the same ordering and type classification as the profiler.
func profileTestColumns(t *testing.T, target ResolvedTarget, names ...string) []profileColumn {
	t.Helper()
	selected, err := profileSelectedColumns(target, names, defaultProfileMaxColumns)
	require.NoError(t, err)
	return selected
}

// profileTestAggregate builds the one-row aggregate result for selected columns:
// total_rows first, then each column's facts in alias order.
func profileTestAggregate(selected []profileColumn, total int64, facts map[string][]any) *db.QueryResult {
	row := make([]any, 0, 1+len(selected)*6)
	row = append(row, total)
	for _, column := range selected {
		row = append(row, facts[column.Name]...)
	}
	return &db.QueryResult{Columns: profileAggregateAliases(selected), Rows: [][]any{row}}
}

// profileTestDefaultFacts is placeholder aggregate data for a column the test does
// not care about.
func profileTestDefaultFacts(column profileColumn) []any {
	facts := []any{int64(10)}
	if column.Facts&profileFactDistinct != 0 {
		facts = append(facts, int64(5))
	}
	if column.Facts&profileFactExtrema != 0 {
		facts = append(facts, int64(1), int64(9))
	}
	if column.Facts&profileFactLength != 0 {
		facts = append(facts, int64(1), int64(3))
	}
	return facts
}

// profileTestSample builds the sample result for selected columns.
func profileTestSample(selected []profileColumn, rows [][]any) *db.QueryResult {
	names := make([]string, 0, len(selected))
	for _, column := range selected {
		names = append(names, column.Name)
	}
	return &db.QueryResult{Columns: names, Rows: rows}
}

// profileTestFacts fills the facts map for every selected column with defaults.
func profileTestFacts(selected []profileColumn) map[string][]any {
	facts := make(map[string][]any, len(selected))
	for _, column := range selected {
		facts[column.Name] = profileTestDefaultFacts(column)
	}
	return facts
}

// TestProfileReadsAggregateFacts pins the mapping from the aggregate row to the
// stored ColumnProfile: exact totals, null count as the difference, distinct
// count, stringified extremes, string length bounds and the sample summaries.
func TestProfileReadsAggregateFacts(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id", "email")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 100, map[string][]any{
			"id":    {int64(95), int64(90), int64(1), int64(999)},
			"email": {int64(80), int64(40), "a@b.c", "z@z.z", int64(3), int64(20)},
		}),
		sample: profileTestSample(selected, [][]any{
			{int64(1), "alice"},
			{int64(2), "bob"},
			{int64(3), nil},
		}),
	}

	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id", "email"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, "limit", profile.SampleMethod)
	require.Equal(t, int64(3), profile.SampleRows)
	require.False(t, profile.Truncated)
	require.Len(t, profile.Columns, 2)

	id := profile.Columns["id"]
	require.NotNil(t, id)
	require.Equal(t, int64(100), id.GetTotalRows())
	require.Equal(t, int64(5), id.GetNullCount())
	require.Equal(t, int64(90), id.GetDistinctCount())
	require.False(t, id.GetDistinctEstimated())
	require.Equal(t, "1", id.GetMinValue())
	require.Equal(t, "999", id.GetMaxValue())
	require.Nil(t, id.MinLength)
	require.Nil(t, id.MaxLength)

	email := profile.Columns["email"]
	require.NotNil(t, email)
	require.Equal(t, int64(100), email.GetTotalRows())
	require.Equal(t, int64(20), email.GetNullCount())
	require.Equal(t, int64(40), email.GetDistinctCount())
	require.Equal(t, "a@b.c", email.GetMinValue())
	require.Equal(t, "z@z.z", email.GetMaxValue())
	require.Equal(t, int64(3), email.GetMinLength())
	require.Equal(t, int64(20), email.GetMaxLength())
	// The email column is above the low-cardinality gate, so no frequent values.
	require.Empty(t, email.GetTopValues())
	require.Equal(t, []string{"alpha"}, email.GetPatterns())

	// Defaults: the aggregate asks for exactly one row, the sample for 1000, both
	// with the 4 MiB / 120s budget.
	require.Equal(t, db.QueryOptions{
		MaxRows:  1,
		MaxBytes: defaultProfileMaxBytes,
		Timeout:  defaultProfileQueryTimeout,
		Purpose:  metrics.QueryPurposeProfileAggregate,
	}, queryer.aggregateOpts)
	require.Equal(t, db.QueryOptions{
		MaxRows:  defaultProfileSampleRows,
		MaxBytes: defaultProfileMaxBytes,
		Timeout:  defaultProfileQueryTimeout,
		Purpose:  metrics.QueryPurposeProfileSample,
	}, queryer.sampleOpts)
}

// TestProfileTopValues pins the low-cardinality gate, the frequency ordering, the
// NULL skip and the 64-byte value truncation (which also merges two long values
// that only differ past the display bound).
func TestProfileTopValues(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "status", "email", "name")
	longA := strings.Repeat("x", 100)
	longB := strings.Repeat("x", 90) + "different"
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 4, map[string][]any{
			"status": {int64(3), int64(4), "a", "z", int64(1), int64(1)},
			"email":  {int64(4), int64(40), "a", "z", int64(1), int64(3)},
			"name":   {int64(4), int64(2), "x", "y", int64(100), int64(100)},
		}),
		sample: profileTestSample(selected, [][]any{
			{"a", "e1", longA},
			{"b", "e2", longB},
			{"a", "e3", "y"},
			{nil, "e4", "y"},
		}),
	}

	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"status", "email", "name"}, ProfileOptions{})
	require.NoError(t, err)

	status := profile.Columns["status"].GetTopValues()
	require.Len(t, status, 2)
	require.Equal(t, "a", status[0].GetValue())
	require.Equal(t, int64(2), status[0].GetCount())
	require.Equal(t, "b", status[1].GetValue())
	require.Equal(t, int64(1), status[1].GetCount())

	// 40 distinct values is above the gate: an identifier is not an enumeration.
	require.Empty(t, profile.Columns["email"].GetTopValues())

	name := profile.Columns["name"].GetTopValues()
	require.Len(t, name, 2)
	// Both long values truncate to the same 64-byte prefix, so they merge.
	require.Len(t, name[0].GetValue(), profileMaxValueBytes)
	require.Equal(t, int64(2), name[0].GetCount())
	require.Equal(t, "y", name[1].GetValue())
	require.Equal(t, int64(2), name[1].GetCount())
}

// TestProfileTopValuesAtMostTen pins the ten-entry cap and the deterministic
// value ordering among equal frequencies.
func TestProfileTopValuesAtMostTen(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "status")
	rows := make([][]any, 0, 15)
	for index := 0; index < 15; index++ {
		rows = append(rows, []any{fmt.Sprintf("v%02d", index)})
	}
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 15, map[string][]any{
			"status": {int64(15), int64(15), "v00", "v14", int64(3), int64(3)},
		}),
		sample: profileTestSample(selected, rows),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"status"}, ProfileOptions{})
	require.NoError(t, err)
	top := profile.Columns["status"].GetTopValues()
	require.Len(t, top, profileMaxTopValues)
	require.Equal(t, "v00", top[0].GetValue())
	require.Equal(t, "v09", top[9].GetValue())
}

// profileRunStringColumn profiles one string column with the given sample and
// distinct count, which is what the pattern table needs.
func profileRunStringColumn(t *testing.T, sampleValues []any, distinct int64) *storepb.ColumnProfile {
	t.Helper()
	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "status")
	rows := make([][]any, 0, len(sampleValues))
	for _, value := range sampleValues {
		rows = append(rows, []any{value})
	}
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, int64(len(sampleValues)), map[string][]any{
			"status": {int64(len(sampleValues)), distinct, "a", "z", int64(1), int64(2)},
		}),
		sample: profileTestSample(selected, rows),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"status"}, ProfileOptions{})
	require.NoError(t, err)
	return profile.Columns["status"]
}

// TestProfilePatterns pins the four shapes: a single shape is reported as itself,
// several shapes collapse to "mixed", and a sample with no non-empty value
// reports nothing at all.
func TestProfilePatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		samples []any
		want    []string
	}{
		{name: "numeric", samples: []any{"1", "2.5", "-3"}, want: []string{"numeric"}},
		{name: "alpha", samples: []any{"alice", "Bob Smith"}, want: []string{"alpha"}},
		{name: "date-like", samples: []any{"2026-01-02", "2026-02-03 04:05:06"}, want: []string{"date-like"}},
		{name: "mixed", samples: []any{"1", "alice"}, want: []string{"mixed"}},
		{name: "no values", samples: []any{nil, "", "   "}, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			column := profileRunStringColumn(t, test.samples, int64(len(test.samples)))
			require.Equal(t, test.want, column.GetPatterns())
		})
	}
}

// TestProfileDefaultsToEveryColumn pins that an empty column request profiles the
// target's whole column list, in registry order.
func TestProfileDefaultsToEveryColumn(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	all, err := profileSelectedColumns(target, nil, defaultProfileMaxColumns)
	require.NoError(t, err)
	require.Len(t, all, len(target.Columns))
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(all, 10, profileTestFacts(all)),
		sample:    profileTestSample(all, nil),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, nil, ProfileOptions{})
	require.NoError(t, err)
	require.Len(t, profile.Columns, len(target.Columns))
	require.Contains(t, queryer.aggregateSQL, "c8_non_null")
}

// TestProfileRejectsInvalidInput pins the validation that must happen before any
// query is issued.
func TestProfileRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)

	t.Run("unknown column", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"nope"}, ProfileOptions{})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Equal(t, 0, queryer.calls)
	})
	t.Run("unknown kind", func(t *testing.T) {
		t.Parallel()
		unknown := target
		unknown.Columns = []ColumnInfo{{Name: "mystery", Kind: ColumnKindUnknown}}
		queryer := &profileFakeQueryer{}
		_, err := NewProfiler().Profile(context.Background(), queryer, unknown, []string{"mystery"}, ProfileOptions{})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Equal(t, 0, queryer.calls)
	})
	t.Run("too many columns", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id", "email"}, ProfileOptions{MaxColumns: 1})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Equal(t, 0, queryer.calls)
	})
	t.Run("no queryer", func(t *testing.T) {
		t.Parallel()
		_, err := NewProfiler().Profile(context.Background(), nil, target, []string{"id"}, ProfileOptions{})
		require.Equal(t, common.Internal, common.ErrorCode(err))
	})
	t.Run("unsupported engine", func(t *testing.T) {
		t.Parallel()
		unsupported := target
		unsupported.Engine = storepb.Engine_ENGINE_UNSPECIFIED
		queryer := &profileFakeQueryer{}
		_, err := NewProfiler().Profile(context.Background(), queryer, unsupported, []string{"id"}, ProfileOptions{})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Equal(t, 0, queryer.calls)
	})
	t.Run("target without columns", func(t *testing.T) {
		t.Parallel()
		empty := target
		empty.Columns = nil
		queryer := &profileFakeQueryer{}
		_, err := NewProfiler().Profile(context.Background(), queryer, empty, nil, ProfileOptions{})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Equal(t, 0, queryer.calls)
	})
}

// TestProfileAggregateShapeViolationsAreBadData pins the contract checks on the
// aggregate row: a missing column, a non-integral count, a row count other than
// one and impossible null arithmetic are BAD_DATA, never a silent zero.
func TestProfileAggregateShapeViolationsAreBadData(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id")

	t.Run("missing column", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{
			aggregate: &db.QueryResult{Columns: []string{"total_rows"}, Rows: [][]any{{int64(1)}}},
			sample:    profileTestSample(selected, nil),
		}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
		require.ErrorIs(t, err, errBadData)
	})
	t.Run("row count not one", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{
			aggregate: &db.QueryResult{
				Columns: profileAggregateAliases(selected),
				Rows:    [][]any{{int64(1), int64(1), int64(1), int64(1), int64(1)}, {int64(1), int64(1), int64(1), int64(1), int64(1)}},
			},
			sample: profileTestSample(selected, nil),
		}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
		require.ErrorIs(t, err, errBadData)
	})
	t.Run("non-integral distinct", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{
			aggregate: profileTestAggregate(selected, 10, map[string][]any{
				"id": {int64(10), float64(1.5), int64(1), int64(9)},
			}),
			sample: profileTestSample(selected, nil),
		}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
		require.ErrorIs(t, err, errBadData)
	})
	t.Run("more non-null values than rows", func(t *testing.T) {
		t.Parallel()
		queryer := &profileFakeQueryer{
			aggregate: profileTestAggregate(selected, 10, map[string][]any{
				"id": {int64(11), int64(11), int64(1), int64(9)},
			}),
			sample: profileTestSample(selected, nil),
		}
		_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
		require.ErrorIs(t, err, errBadData)
	})
}

// TestProfileSampleFailureIsAnError pins that a failed sample query fails the
// whole profile: returning aggregate facts without their sample summaries would
// present an incomplete profile as complete.
func TestProfileSampleFailureIsAnError(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id")
	sampleErr := errors.New("sample query failed")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, profileTestFacts(selected)),
		sampleErr: sampleErr,
		sample:    nil,
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
	require.ErrorIs(t, err, sampleErr)
	require.Nil(t, profile)
	require.Equal(t, 2, queryer.calls)

	// A sample result of nil without an error is a contract violation.
	queryer = &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, profileTestFacts(selected)),
	}
	_, err = NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
	require.ErrorIs(t, err, errBadData)
}

// TestProfileTruncatedFlag pins that a truncated aggregate or sample marks the
// whole profile truncated.
func TestProfileTruncatedFlag(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id")

	for _, test := range []struct {
		name      string
		aggregate *db.QueryResult
		sample    *db.QueryResult
		want      bool
	}{
		{
			name:      "aggregate truncated",
			aggregate: &db.QueryResult{Columns: profileAggregateAliases(selected), Rows: [][]any{{int64(1), int64(1), int64(1), int64(1), int64(1)}}, Truncated: true},
			sample:    profileTestSample(selected, nil),
			want:      true,
		},
		{
			name:      "sample truncated",
			aggregate: profileTestAggregate(selected, 1, map[string][]any{"id": {int64(1), int64(1), int64(1), int64(9)}}),
			sample:    &db.QueryResult{Columns: []string{"id"}},
			want:      true,
		},
		{
			name:      "nothing truncated",
			aggregate: profileTestAggregate(selected, 1, map[string][]any{"id": {int64(1), int64(1), int64(1), int64(9)}}),
			sample:    profileTestSample(selected, nil),
			want:      false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sample := test.sample
			if test.name == "sample truncated" {
				sample.Truncated = true
			}
			queryer := &profileFakeQueryer{aggregate: test.aggregate, sample: sample}
			profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
			require.NoError(t, err)
			require.Equal(t, test.want, profile.Truncated)
		})
	}
}

// TestProfileSQLGoldens pins the complete aggregate and sample statements on the
// three dialect families, plus the query bounds each one runs with.
func TestProfileSQLGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engine       storepb.Engine
		aggregateSQL string
		sampleSQL    string
	}{
		{
			engine: storepb.Engine_MYSQL,
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT(`id`) AS c0_non_null, " +
				"COUNT(DISTINCT `id`) AS c0_distinct, MIN(`id`) AS c0_min, MAX(`id`) AS c0_max, " +
				"COUNT(`email`) AS c1_non_null, COUNT(DISTINCT `email`) AS c1_distinct, " +
				"MIN(`email`) AS c1_min, MAX(`email`) AS c1_max, " +
				"MIN(CHAR_LENGTH(`email`)) AS c1_min_len, MAX(CHAR_LENGTH(`email`)) AS c1_max_len " +
				"FROM `shop`.`orders`",
			sampleSQL: "SELECT `id`, `email` FROM `shop`.`orders` LIMIT 1000",
		},
		{
			engine: storepb.Engine_POSTGRES,
			aggregateSQL: `SELECT COUNT(*) AS total_rows, COUNT("id") AS c0_non_null, ` +
				`COUNT(DISTINCT "id") AS c0_distinct, MIN("id") AS c0_min, MAX("id") AS c0_max, ` +
				`COUNT("email") AS c1_non_null, COUNT(DISTINCT "email") AS c1_distinct, ` +
				`MIN("email") AS c1_min, MAX("email") AS c1_max, ` +
				`MIN(CHAR_LENGTH("email")) AS c1_min_len, MAX(CHAR_LENGTH("email")) AS c1_max_len ` +
				`FROM "public"."orders"`,
			sampleSQL: `SELECT "id", "email" FROM "public"."orders" LIMIT 1000`,
		},
		{
			engine: storepb.Engine_MSSQL,
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT([id]) AS c0_non_null, " +
				"COUNT(DISTINCT [id]) AS c0_distinct, MIN([id]) AS c0_min, MAX([id]) AS c0_max, " +
				"COUNT([email]) AS c1_non_null, COUNT(DISTINCT [email]) AS c1_distinct, " +
				"MIN([email]) AS c1_min, MAX([email]) AS c1_max, " +
				"MIN(LEN([email])) AS c1_min_len, MAX(LEN([email])) AS c1_max_len " +
				"FROM [public].[orders]",
			sampleSQL: "SELECT [id], [email] FROM [public].[orders] ORDER BY (SELECT NULL) " +
				"OFFSET 0 ROWS FETCH NEXT 1000 ROWS ONLY",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			target := compileTestTarget(test.engine)
			selected := profileTestColumns(t, target, "id", "email")
			queryer := &profileFakeQueryer{
				aggregate: profileTestAggregate(selected, 7, map[string][]any{
					"id":    {int64(7), int64(7), int64(1), int64(7)},
					"email": {int64(6), int64(4), "a", "z", int64(1), int64(3)},
				}),
				sample: profileTestSample(selected, [][]any{{int64(1), "a"}}),
			}
			profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id", "email"}, ProfileOptions{})
			require.NoError(t, err)
			require.Equal(t, test.aggregateSQL, queryer.aggregateSQL)
			require.Equal(t, test.sampleSQL, queryer.sampleSQL)
			require.Equal(t, "limit", profile.SampleMethod)
			require.Equal(t, int64(1), profile.SampleRows)
			require.Equal(t, int64(7), profile.Columns["id"].GetTotalRows())
		})
	}
}

// profileMixedKindTarget is the golden relation with a numeric, a boolean and a
// text column, so the tests can pin how the aggregate's alias groups stay aligned
// when one kind contributes fewer expressions.
func profileMixedKindTarget(engine storepb.Engine) ResolvedTarget {
	target := compileTestTarget(engine)
	target.Columns = []ColumnInfo{
		{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		{Name: "enabled", Type: "boolean", Kind: ColumnKindBoolean, Nullable: true},
		{Name: "email", Type: "varchar(255)", Kind: ColumnKindString},
	}
	return target
}

// TestProfileBooleanColumnOmitsExtremes pins the boolean special case: neither
// PostgreSQL nor SQL Server has a MIN/MAX aggregate for a boolean, so the
// aggregate only counts the column and the sample's frequent values describe it.
func TestProfileBooleanColumnOmitsExtremes(t *testing.T) {
	t.Parallel()

	target := profileMixedKindTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "enabled")
	require.Equal(t, []string{"total_rows", "c0_non_null", "c0_distinct"}, profileAggregateAliases(selected))

	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, map[string][]any{
			"enabled": {int64(8), int64(2)},
		}),
		sample: profileTestSample(selected, [][]any{{true}, {false}, {true}, {nil}}),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"enabled"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, COUNT("enabled") AS c0_non_null, `+
		`COUNT(DISTINCT "enabled") AS c0_distinct FROM "public"."orders"`, queryer.aggregateSQL)
	require.Equal(t, `SELECT "enabled" FROM "public"."orders" LIMIT 1000`, queryer.sampleSQL)

	enabled := profile.Columns["enabled"]
	require.Equal(t, int64(10), enabled.GetTotalRows())
	require.Equal(t, int64(2), enabled.GetNullCount())
	require.Equal(t, int64(2), enabled.GetDistinctCount())
	// No extreme and no length is read for a boolean.
	require.Empty(t, enabled.GetMinValue())
	require.Empty(t, enabled.GetMaxValue())
	require.Nil(t, enabled.MinLength)
	require.Nil(t, enabled.MaxLength)
	require.Empty(t, enabled.GetPatterns())
	// The sample still gives the low-cardinality distribution.
	require.Len(t, enabled.GetTopValues(), 2)
	require.Equal(t, "true", enabled.GetTopValues()[0].GetValue())
	require.Equal(t, int64(2), enabled.GetTopValues()[0].GetCount())
	require.Equal(t, "false", enabled.GetTopValues()[1].GetValue())
	require.Equal(t, int64(1), enabled.GetTopValues()[1].GetCount())
}

// TestProfileBooleanColumnSQLGoldens pins the boolean aggregate and sample SQL on
// the three dialect families: the aggregate has no MIN/MAX expression at all and
// the sample still selects just the column.
func TestProfileBooleanColumnSQLGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engine       storepb.Engine
		aggregateSQL string
		sampleSQL    string
	}{
		{
			engine:       storepb.Engine_MYSQL,
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT(`enabled`) AS c0_non_null, COUNT(DISTINCT `enabled`) AS c0_distinct FROM `shop`.`orders`",
			sampleSQL:    "SELECT `enabled` FROM `shop`.`orders` LIMIT 1000",
		},
		{
			engine:       storepb.Engine_POSTGRES,
			aggregateSQL: `SELECT COUNT(*) AS total_rows, COUNT("enabled") AS c0_non_null, COUNT(DISTINCT "enabled") AS c0_distinct FROM "public"."orders"`,
			sampleSQL:    `SELECT "enabled" FROM "public"."orders" LIMIT 1000`,
		},
		{
			engine:       storepb.Engine_MSSQL,
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT([enabled]) AS c0_non_null, COUNT(DISTINCT [enabled]) AS c0_distinct FROM [public].[orders]",
			sampleSQL:    "SELECT [enabled] FROM [public].[orders] ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 1000 ROWS ONLY",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			target := profileMixedKindTarget(test.engine)
			selected := profileTestColumns(t, target, "enabled")
			queryer := &profileFakeQueryer{
				aggregate: profileTestAggregate(selected, 4, map[string][]any{
					"enabled": {int64(3), int64(2)},
				}),
				sample: profileTestSample(selected, [][]any{{true}}),
			}
			profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"enabled"}, ProfileOptions{})
			require.NoError(t, err)
			require.Equal(t, test.aggregateSQL, queryer.aggregateSQL)
			require.Equal(t, test.sampleSQL, queryer.sampleSQL)
			require.Equal(t, int64(4), profile.Columns["enabled"].GetTotalRows())
		})
	}
}

// TestProfileMixedKindsAggregateLayout pins that a boolean's shorter alias group
// does not shift the columns that follow it: the numeric extremes and the text
// length bounds are read from their own aliases.
func TestProfileMixedKindsAggregateLayout(t *testing.T) {
	t.Parallel()

	target := profileMixedKindTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id", "enabled", "email")
	require.Equal(t, []string{
		"total_rows",
		"c0_non_null", "c0_distinct", "c0_min", "c0_max",
		"c1_non_null", "c1_distinct",
		"c2_non_null", "c2_distinct", "c2_min", "c2_max", "c2_min_len", "c2_max_len",
	}, profileAggregateAliases(selected))

	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, map[string][]any{
			"id":      {int64(10), int64(10), int64(1), int64(9)},
			"enabled": {int64(9), int64(2)},
			"email":   {int64(8), int64(5), "a", "z", int64(1), int64(3)},
		}),
		sample: profileTestSample(selected, [][]any{{int64(1), true, "a"}}),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id", "enabled", "email"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, COUNT("id") AS c0_non_null, `+
		`COUNT(DISTINCT "id") AS c0_distinct, MIN("id") AS c0_min, MAX("id") AS c0_max, `+
		`COUNT("enabled") AS c1_non_null, COUNT(DISTINCT "enabled") AS c1_distinct, `+
		`COUNT("email") AS c2_non_null, COUNT(DISTINCT "email") AS c2_distinct, `+
		`MIN("email") AS c2_min, MAX("email") AS c2_max, `+
		`MIN(CHAR_LENGTH("email")) AS c2_min_len, MAX(CHAR_LENGTH("email")) AS c2_max_len `+
		`FROM "public"."orders"`, queryer.aggregateSQL)
	require.Equal(t, "1", profile.Columns["id"].GetMinValue())
	require.Equal(t, "9", profile.Columns["id"].GetMaxValue())
	require.Empty(t, profile.Columns["enabled"].GetMinValue())
	require.Empty(t, profile.Columns["enabled"].GetMaxValue())
	require.Equal(t, "a", profile.Columns["email"].GetMinValue())
	require.Equal(t, "z", profile.Columns["email"].GetMaxValue())
	require.Equal(t, int64(1), profile.Columns["email"].GetMinLength())
	require.Equal(t, int64(3), profile.Columns["email"].GetMaxLength())
}

// TestProfileOptionsDefaults pins the four defaults and that explicit values
// reach the two queries unchanged.
func TestProfileOptionsDefaults(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	selected := profileTestColumns(t, target, "id")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, profileTestFacts(selected)),
		sample:    profileTestSample(selected, nil),
	}
	_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, 1000, queryer.sampleOpts.MaxRows)
	require.Equal(t, 4<<20, queryer.aggregateOpts.MaxBytes)
	require.Equal(t, 120*time.Second, queryer.aggregateOpts.Timeout)

	queryer = &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 10, profileTestFacts(selected)),
		sample:    profileTestSample(selected, nil),
	}
	_, err = NewProfiler().Profile(context.Background(), queryer, target, []string{"id"}, ProfileOptions{
		MaxColumns:   1,
		SampleRows:   25,
		QueryTimeout: 5 * time.Second,
		MaxBytes:     1024,
	})
	require.NoError(t, err)
	require.Equal(t, 25, queryer.sampleOpts.MaxRows)
	require.Equal(t, 1024, queryer.aggregateOpts.MaxBytes)
	require.Equal(t, 5*time.Second, queryer.sampleOpts.Timeout)
}

// profileFactTarget is a one-column relation whose column has the given type.
func profileFactTarget(engine storepb.Engine, columnType string) ResolvedTarget {
	target := compileTestTarget(engine)
	target.Columns = []ColumnInfo{{
		Name: "payload",
		Type: columnType,
		Kind: classifyColumnTypeForEngine(engine, columnType),
	}}
	return target
}

// TestProfileFactsFor pins which aggregate facts each engine type supports. The
// profiler derives them from the same capability table the rule validators use, so
// a type that cannot answer a question is never asked it.
func TestProfileFactsFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engine     storepb.Engine
		columnType string
		want       profileFacts
	}{
		{storepb.Engine_POSTGRES, "varchar(255)", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_POSTGRES, "character varying", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_POSTGRES, "text", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_POSTGRES, "name", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_POSTGRES, "public.citext", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_POSTGRES, "jsonb", profileFactDistinct},
		{storepb.Engine_POSTGRES, "json", 0},
		{storepb.Engine_POSTGRES, "xml", 0},
		{storepb.Engine_POSTGRES, "uuid", profileFactDistinct},
		{storepb.Engine_POSTGRES, "bytea", profileFactDistinct},
		{storepb.Engine_POSTGRES, "interval", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_POSTGRES, "_int4", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_POSTGRES, "integer[]", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_POSTGRES, "macaddr", profileFactDistinct},
		{storepb.Engine_POSTGRES, "bit(8)", profileFactDistinct},
		{storepb.Engine_POSTGRES, "_box", 0},
		{storepb.Engine_POSTGRES, "box[]", 0},
		{storepb.Engine_POSTGRES, "public.mood", 0},
		{storepb.Engine_POSTGRES, "bigint", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_POSTGRES, "boolean", profileFactDistinct},
		{storepb.Engine_MYSQL, "varchar(255)", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_MYSQL, "blob", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_MYSQL, "json", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_MYSQL, "geometry", profileFactDistinct},
		{storepb.Engine_STARROCKS, "json", profileFactLength},
		{storepb.Engine_STARROCKS, "array", profileFactDistinct},
		{storepb.Engine_STARROCKS, "array<json>", 0},
		{storepb.Engine_STARROCKS, "array<variant>", 0},
		{storepb.Engine_STARROCKS, "bitmap", profileFactDistinct},
		{storepb.Engine_STARROCKS, "percentile", 0},
		{storepb.Engine_STARROCKS, "largeint", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_MSSQL, "nvarchar(255)", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_MSSQL, "text", 0},
		{storepb.Engine_MSSQL, "ntext", 0},
		{storepb.Engine_MSSQL, "xml", 0},
		{storepb.Engine_MSSQL, "uniqueidentifier", profileFactDistinct | profileFactExtrema | profileFactLength},
		{storepb.Engine_MSSQL, "datetime", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_MSSQL, "datetimeoffset", profileFactDistinct | profileFactExtrema},
		{storepb.Engine_MSSQL, "bit", profileFactDistinct},
		{storepb.Engine_MSSQL, "hierarchyid", profileFactDistinct | profileFactExtrema},
	}
	for _, test := range tests {
		t.Run(test.engine.String()+"/"+test.columnType, func(t *testing.T) {
			t.Parallel()
			target := profileFactTarget(test.engine, test.columnType)
			selected := profileTestColumns(t, target, "payload")
			require.Len(t, selected, 1)
			require.Equal(t, test.want, selected[0].Facts)
			require.Equal(t, test.want&profileFactDistinct == 0, selected[0].EstimatedDistinct)
		})
	}
}

// TestProfileZeroFactColumnSQLGolden pins the non-NULL count for a type with no
// aggregate facts: SQL Server rejects COUNT on text/ntext/image, so those use a CASE
// expression, while a type whose COUNT is valid (PostgreSQL json, StarRocks bitmap)
// uses COUNT(col) — the bitmap type rejects the CASE predicate entirely.
func TestProfileZeroFactColumnSQLGolden(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		engine       storepb.Engine
		columnType   string
		aggregateSQL string
		sampleSQL    string
	}{
		{
			engine:       storepb.Engine_MSSQL,
			columnType:   "text",
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT(CASE WHEN [payload] IS NOT NULL THEN 1 END) AS c0_non_null FROM [public].[orders]",
			sampleSQL: "SELECT [payload] FROM [public].[orders] ORDER BY (SELECT NULL) " +
				"OFFSET 0 ROWS FETCH NEXT 1000 ROWS ONLY",
		},
		{
			engine:       storepb.Engine_POSTGRES,
			columnType:   "json",
			aggregateSQL: `SELECT COUNT(*) AS total_rows, COUNT("payload") AS c0_non_null FROM "public"."orders"`,
			sampleSQL:    `SELECT "payload" FROM "public"."orders" LIMIT 1000`,
		},
		{
			engine:       storepb.Engine_STARROCKS,
			columnType:   "percentile",
			aggregateSQL: "SELECT COUNT(*) AS total_rows, COUNT(`payload`) AS c0_non_null FROM `shop`.`orders`",
			sampleSQL:    "SELECT `payload` FROM `shop`.`orders` LIMIT 1000",
		},
	} {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			target := profileFactTarget(test.engine, test.columnType)
			selected := profileTestColumns(t, target, "payload")
			require.Equal(t, []string{"total_rows", "c0_non_null"}, profileAggregateAliases(selected))
			queryer := &profileFakeQueryer{
				aggregate: profileTestAggregate(selected, 0, map[string][]any{"payload": {int64(0)}}),
				sample:    profileTestSample(selected, nil),
			}
			_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
			require.NoError(t, err)
			require.Equal(t, test.aggregateSQL, queryer.aggregateSQL)
			require.Equal(t, test.sampleSQL, queryer.sampleSQL)
		})
	}
}

// TestProfileStarRocksBitmapSQLGolden pins that a type which rejects predicates but
// supports COUNT and COUNT(DISTINCT) is profiled without a CASE expression.
func TestProfileStarRocksBitmapSQLGolden(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_STARROCKS, "bitmap")
	selected := profileTestColumns(t, target, "payload")
	require.Equal(t, []string{"total_rows", "c0_non_null", "c0_distinct"}, profileAggregateAliases(selected))
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 0, map[string][]any{"payload": {int64(0), int64(0)}}),
		sample:    profileTestSample(selected, nil),
	}
	_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, "SELECT COUNT(*) AS total_rows, COUNT(`payload`) AS c0_non_null, "+
		"COUNT(DISTINCT `payload`) AS c0_distinct FROM `shop`.`orders`", queryer.aggregateSQL)
}

// TestProfileHexRendersBinaryValues pins that binary extrema/top values stay valid
// UTF-8 for the stored protojson (SQL Server varbinary, MySQL blob).
func TestProfileHexRendersBinaryValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0xfffe", profileValueString([]byte{0xff, 0xfe}, true))
	require.Equal(t, "plain", profileValueString("plain", false))
	// A binary value that happens to spell "0xfffe" must not collide with the hex
	// rendering of the bytes ff fe.
	require.NotEqual(t,
		profileValueString([]byte("0xfffe"), true),
		profileValueString([]byte{0xff, 0xfe}, true))
}

// TestProfileCharacterArraySQLGolden pins blocker B-1 from the third review: a
// materialized view stores "character varying[]", which must be profiled as an array
// (no CHAR_LENGTH), not as a text scalar.
func TestProfileCharacterArraySQLGolden(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_POSTGRES, "character varying[]")
	selected := profileTestColumns(t, target, "payload")
	require.Equal(t, []string{"total_rows", "c0_non_null", "c0_distinct", "c0_min", "c0_max"}, profileAggregateAliases(selected))
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 0, map[string][]any{"payload": {int64(0), int64(0), nil, nil}}),
		sample:    profileTestSample(selected, nil),
	}
	_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, COUNT("payload") AS c0_non_null, `+
		`COUNT(DISTINCT "payload") AS c0_distinct, MIN("payload") AS c0_min, MAX("payload") AS c0_max `+
		`FROM "public"."orders"`, queryer.aggregateSQL)
}

// TestProfileStarRocksNestedJSONArraySQLGolden pins blocker B-2: a nested-json array
// must not be claimed groupable, so no COUNT(DISTINCT) is emitted.
func TestProfileStarRocksNestedJSONArraySQLGolden(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_STARROCKS, "array<struct<a json>>")
	selected := profileTestColumns(t, target, "payload")
	require.Equal(t, []string{"total_rows", "c0_non_null"}, profileAggregateAliases(selected))
}

// TestProfileJSONBColumnSQLGolden pins that a jsonb column keeps an exact distinct
// count (PostgreSQL defines equality for it) while emitting no MIN/MAX or length.
func TestProfileJSONBColumnSQLGolden(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_POSTGRES, "jsonb")
	selected := profileTestColumns(t, target, "payload")
	require.Equal(t, []string{"total_rows", "c0_non_null", "c0_distinct"}, profileAggregateAliases(selected))
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 0, map[string][]any{"payload": {int64(0), int64(0)}}),
		sample:    profileTestSample(selected, nil),
	}
	_, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, COUNT("payload") AS c0_non_null, `+
		`COUNT(DISTINCT "payload") AS c0_distinct FROM "public"."orders"`, queryer.aggregateSQL)
}

// TestProfileMySQLJSONColumnSQLGolden pins that a MySQL JSON column keeps every
// fact its engine supports: MIN/MAX, length and exact distinct all work there.
func TestProfileMySQLJSONColumnSQLGolden(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_MYSQL, "json")
	selected := profileTestColumns(t, target, "payload")
	require.Equal(t, []string{
		"total_rows", "c0_non_null", "c0_distinct", "c0_min", "c0_max", "c0_min_len", "c0_max_len",
	}, profileAggregateAliases(selected))
}

// TestProfileCountedColumnFromSample pins the profile of a type with no aggregate
// facts: exact totals from the aggregate, the distinct count and frequent values
// from the sample, and no extremes, lengths or patterns.
func TestProfileCountedColumnFromSample(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_POSTGRES, "json")
	selected := profileTestColumns(t, target, "payload")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 500, map[string][]any{"payload": {int64(400)}}),
		sample: profileTestSample(selected, [][]any{
			{[]byte(`{"kind":"audit"}`)},
			{[]byte(`{"kind":"login"}`)},
			{[]byte(`{"kind":"audit"}`)},
			{nil},
		}),
	}

	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(4), profile.SampleRows)

	payload := profile.Columns["payload"]
	require.Equal(t, int64(500), payload.GetTotalRows())
	require.Equal(t, int64(100), payload.GetNullCount())
	require.Equal(t, int64(2), payload.GetDistinctCount())
	require.True(t, payload.GetDistinctEstimated(), "the sample read fewer rows than the table has")
	require.Empty(t, payload.GetMinValue())
	require.Empty(t, payload.GetMaxValue())
	require.Nil(t, payload.MinLength)
	require.Nil(t, payload.MaxLength)
	require.Empty(t, payload.GetPatterns(), "a document column has no text shape")
	require.Len(t, payload.GetTopValues(), 2)
	require.Equal(t, `{"kind":"audit"}`, payload.GetTopValues()[0].GetValue())
	require.Equal(t, int64(2), payload.GetTopValues()[0].GetCount())
	require.Equal(t, `{"kind":"login"}`, payload.GetTopValues()[1].GetValue())
}

// TestProfileCountedColumnExactSample pins that a sample which saw every row is an
// exact distinct count, not an estimate.
func TestProfileCountedColumnExactSample(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_POSTGRES, "json")
	selected := profileTestColumns(t, target, "payload")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 3, map[string][]any{"payload": {int64(3)}}),
		sample: profileTestSample(selected, [][]any{
			{[]byte(`{"a":1}`)},
			{[]byte(`{"a":1}`)},
			{[]byte(`{"b":2}`)},
		}),
	}

	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	payload := profile.Columns["payload"]
	require.Equal(t, int64(2), payload.GetDistinctCount())
	require.False(t, payload.GetDistinctEstimated())
}

// TestProfileTextShapedColumnFromSample pins that a non-countable type that renders
// as text still gets its value shapes from the sample (SQL Server's legacy text).
func TestProfileTextShapedColumnFromSample(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_MSSQL, "text")
	selected := profileTestColumns(t, target, "payload")
	queryer := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 2, map[string][]any{"payload": {int64(2)}}),
		sample: profileTestSample(selected, [][]any{
			{[]byte("alice")},
			{[]byte("bob")},
		}),
	}
	profile, err := NewProfiler().Profile(context.Background(), queryer, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	payload := profile.Columns["payload"]
	require.Equal(t, []string{"alpha"}, payload.GetPatterns())
	require.Equal(t, []string{"total_rows", "c0_non_null"}, profileAggregateAliases(selected))
}

// TestProfileEstimatedDistinctSignals pins the two edges of the sample-based count:
// a truncated sample is an estimate even when it read the table's row count, and a
// column with no values keeps a zero distinct count.
func TestProfileEstimatedDistinctSignals(t *testing.T) {
	t.Parallel()

	target := profileFactTarget(storepb.Engine_POSTGRES, "json")
	selected := profileTestColumns(t, target, "payload")

	truncated := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 2, map[string][]any{"payload": {int64(2)}}),
		sample: &db.QueryResult{
			Columns:   []string{"payload"},
			Rows:      [][]any{{[]byte(`{"a":1}`)}, {[]byte(`{"b":2}`)}},
			Truncated: true,
		},
	}
	profile, err := NewProfiler().Profile(context.Background(), truncated, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	require.True(t, profile.Columns["payload"].GetDistinctEstimated())

	allNull := &profileFakeQueryer{
		aggregate: profileTestAggregate(selected, 5, map[string][]any{"payload": {int64(0)}}),
		sample:    profileTestSample(selected, [][]any{{nil}, {nil}}),
	}
	profile, err = NewProfiler().Profile(context.Background(), allNull, target, []string{"payload"}, ProfileOptions{})
	require.NoError(t, err)
	payload := profile.Columns["payload"]
	require.Equal(t, int64(5), payload.GetNullCount())
	require.Equal(t, int64(0), payload.GetDistinctCount())
	require.True(t, payload.GetDistinctEstimated())
}
