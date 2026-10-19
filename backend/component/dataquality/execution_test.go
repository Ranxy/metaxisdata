package dataquality

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// execFakeQueryer stands in for a read-only driver: it records every statement
// and option it is handed and answers from a pre-queued list, so the executor is
// tested without a database.
type execFakeQueryer struct {
	results []*db.QueryResult
	errs    []error
	queries []string
	options []db.QueryOptions
	next    int
}

func (f *execFakeQueryer) QueryReadOnly(_ context.Context, query string, opts db.QueryOptions) (*db.QueryResult, error) {
	index := f.next
	f.next++
	f.queries = append(f.queries, query)
	f.options = append(f.options, opts)
	if index < len(f.errs) && f.errs[index] != nil {
		return nil, f.errs[index]
	}
	if index < len(f.results) {
		return f.results[index], nil
	}
	return nil, errors.New("the fake queryer has no result queued for this call")
}

// execTestResolution is a resolved target for one engine. Only the target matters
// to the executor; the instance and metadata are the resolver's concern.
func execTestResolution(engine storepb.Engine) *Resolution {
	return &Resolution{Target: compileTestTarget(engine)}
}

// execTestSnapshot builds the rule snapshot one execution is judged against.
func execTestSnapshot(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) RuleSnapshot {
	return RuleSnapshot{
		ID:        1,
		Revision:  1,
		Type:      ruleType,
		Dimension: storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		Severity:  storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		Config:    config,
	}
}

// aggregateRow is one aggregate result with the given columns and values.
func aggregateRow(columns []string, values ...any) *db.QueryResult {
	return &db.QueryResult{Columns: columns, Rows: [][]any{values}}
}

// execWantObservation is the expectation for one rule's interpretation.
type execWantObservation struct {
	total    int64
	failed   int64
	observed float64
	kind     storepb.QualityObservedKind
	maxTS    *time.Time
}

// TestExecuteInterpretsEveryRuleType runs each of the 13 rule types through the
// executor and pins what its aggregate row becomes. The columns and configs come
// from the compiler goldens, so a change to either contract fails here too.
func TestExecuteInterpretsEveryRuleType(t *testing.T) {
	t.Parallel()

	maxTimestamp := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	wantByName := map[string]execWantObservation{
		"row_count": {
			total: 42, observed: 42,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"row_count_change": {
			total: 42, observed: 42,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"null_percent": {
			total: 1200, failed: 3, observed: 3,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"unique": {
			total: 1200, failed: 8, observed: 8,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"unique_combination": {
			total: 900, failed: 4, observed: 4,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"accepted_values": {
			total: 100, failed: 5, observed: 5,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"range": {
			total: 100, failed: 7, observed: 7,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"string_length": {
			total: 100, failed: 2, observed: 2,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"referential_integrity": {
			total: 50, failed: 3, observed: 3,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"cross_column": {
			total: 80, failed: 1, observed: 1,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
		"freshness": {
			total: 100, observed: 3600, maxTS: &maxTimestamp,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS,
		},
		"custom_sql": {
			failed: 9, observed: 9,
			kind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		},
	}
	// One aggregate row per rule name, in the order the compiler declares its
	// columns. The integer values are what a driver normalizes COUNT to; the
	// float form is covered separately.
	rowsByName := map[string][]any{
		"row_count":             {int64(42)},
		"row_count_change":      {int64(42)},
		"null_percent":          {int64(1200), int64(3)},
		"unique":                {int64(8), int64(2), int64(1200)},
		"unique_combination":    {int64(4), int64(2), int64(900)},
		"accepted_values":       {int64(100), int64(5)},
		"range":                 {int64(100), int64(7)},
		"string_length":         {int64(100), int64(2)},
		"referential_integrity": {int64(50), int64(3)},
		"cross_column":          {int64(80), int64(1)},
		"freshness":             {int64(100), maxTimestamp, int64(3600)},
		"custom_sql":            {int64(9)},
	}

	for _, golden := range compileRuleGoldens() {
		want, ok := wantByName[golden.name]
		require.Truef(t, ok, "the golden %q has no interpretation expectation", golden.name)
		row, ok := rowsByName[golden.name]
		require.Truef(t, ok, "the golden %q has no aggregate row", golden.name)

		t.Run(golden.name, func(t *testing.T) {
			t.Parallel()
			queryer := &execFakeQueryer{results: []*db.QueryResult{aggregateRow(golden.wantColumns, row...)}}
			execution, err := NewExecutor().Execute(
				context.Background(),
				queryer,
				execTestResolution(storepb.Engine_MYSQL),
				execTestSnapshot(golden.ruleType, golden.config),
				ExecOptions{},
			)
			require.NoError(t, err)
			require.Len(t, queryer.queries, 1, "an execution without samples runs exactly one query")
			require.Equal(t, 1, queryer.options[0].MaxRows, "an aggregate observation is one row")
			require.Contains(t, execution.ExecutedSQL, "FROM")

			observation := execution.Observation
			require.Equal(t, want.total, observation.TotalRows)
			require.Equal(t, want.failed, observation.FailedRows)
			require.InDelta(t, want.observed, observation.Observed, 1e-9)
			require.Equal(t, want.kind, observation.ObservedKind)
			if want.maxTS == nil {
				require.Nil(t, observation.MaxTimestamp)
			} else {
				require.NotNil(t, observation.MaxTimestamp)
				require.True(t, want.maxTS.Equal(*observation.MaxTimestamp))
			}
			require.False(t, execution.Sampled, "v1 has no sampling rule")
			require.Nil(t, execution.Samples)
		})
	}
}

// A PostgreSQL numeric SUM normalizes to float64, and MySQL's DECIMAL does too;
// the interpretation must accept that shape, not only int64.
func TestExecuteAcceptsNormalizedNumericForms(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	for _, test := range []struct {
		name string
		row  []any
	}{
		{name: "integers", row: []any{int64(1200), int64(3)}},
		{name: "floats from numeric", row: []any{float64(1200), float64(3)}},
		{name: "numeric strings", row: []any{"1200", "3"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queryer := &execFakeQueryer{results: []*db.QueryResult{
				aggregateRow([]string{"total_rows", "failed_rows"}, test.row...),
			}}
			execution, err := NewExecutor().Execute(context.Background(), queryer,
				execTestResolution(storepb.Engine_POSTGRES),
				execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
				ExecOptions{})
			require.NoError(t, err)
			require.Equal(t, int64(1200), execution.Observation.TotalRows)
			require.Equal(t, int64(3), execution.Observation.FailedRows)
			require.InDelta(t, 3, execution.Observation.Observed, 1e-9)
		})
	}
}

// A NULL failed_rows is what SUM(CASE …) returns over zero rows: it means no
// failure, not a broken result.
func TestExecuteReadsNullFailedRowsAsZero(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(0), nil),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
		ExecOptions{})
	require.NoError(t, err)
	require.Zero(t, execution.Observation.FailedRows)
	require.Zero(t, execution.Observation.TotalRows)
}

// freshness with no timestamp is missing data: the observation keeps the total
// and leaves MaxTimestamp nil, and the evaluator's on_empty decides.
func TestExecuteFreshnessWithoutATimestamp(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
		Freshness: &storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: 3600},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "max_ts", "lag_seconds"}, int64(0), nil, nil),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, config),
		ExecOptions{})
	require.NoError(t, err)
	require.Zero(t, execution.Observation.TotalRows)
	require.Nil(t, execution.Observation.MaxTimestamp)
	require.Zero(t, execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS, execution.Observation.ObservedKind)
}

// A result that does not match the compiled statement is BAD_DATA, never a zero
// that reads as a passing check.
func TestExecuteRejectsAMalformedAggregate(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	rule := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config)

	for _, test := range []struct {
		name   string
		result *db.QueryResult
	}{
		{
			name:   "missing failed_rows",
			result: aggregateRow([]string{"total_rows"}, int64(10)),
		},
		{
			name:   "wrong type",
			result: aggregateRow([]string{"total_rows", "failed_rows"}, int64(10), "not-a-number"),
		},
		{
			name:   "fractional count",
			result: aggregateRow([]string{"total_rows", "failed_rows"}, int64(10), 1.5),
		},
		{
			name:   "no row",
			result: &db.QueryResult{Columns: []string{"total_rows", "failed_rows"}},
		},
		{
			name: "two rows",
			result: &db.QueryResult{
				Columns: []string{"total_rows", "failed_rows"},
				Rows:    [][]any{{int64(1), int64(0)}, {int64(1), int64(0)}},
			},
		},
		{name: "nil result", result: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queryer := &execFakeQueryer{results: []*db.QueryResult{test.result}}
			_, err := NewExecutor().Execute(context.Background(), queryer,
				execTestResolution(storepb.Engine_POSTGRES), rule, ExecOptions{})
			require.Error(t, err)
			require.ErrorIs(t, err, errBadData)
			code, _ := ClassifyError(err)
			require.Equal(t, ErrCodeBadData, code)
		})
	}
}

// A truncated aggregate is still a usable observation, but the result must say it
// was cut off.
func TestExecuteCarriesTheAggregateTruncation(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns:   []string{"total_rows"},
		Rows:      [][]any{{int64(5)}},
		Truncated: true,
	}}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config),
		ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Truncated)
	require.True(t, execution.Observation.Truncated)
}

// A driver error is returned unchanged so the caller can classify it; a timeout
// becomes TIMEOUT.
func TestExecutePropagatesAndClassifiesADriverError(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
	queryer := &execFakeQueryer{errs: []error{context.DeadlineExceeded}}
	_, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config),
		ExecOptions{})
	require.Error(t, err)
	code, message := ClassifyError(err)
	require.Equal(t, ErrCodeTimeout, code)
	require.NotEmpty(t, message)
}

// Execute validates its inputs before it touches the queryer: a nil queryer or
// resolution is a caller bug, and a missing config is a malformed rule.
func TestExecuteRequiresItsInputs(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
	rule := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config)
	resolution := execTestResolution(storepb.Engine_MYSQL)
	queryer := &execFakeQueryer{}

	_, err := NewExecutor().Execute(context.Background(), nil, resolution, rule, ExecOptions{})
	require.Equal(t, common.Internal, common.ErrorCode(err))

	_, err = NewExecutor().Execute(context.Background(), queryer, nil, rule, ExecOptions{})
	require.Equal(t, common.Internal, common.ErrorCode(err))

	_, err = NewExecutor().Execute(context.Background(), queryer, resolution,
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, nil), ExecOptions{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// A percentage threshold needs a denominator. Types that only count failed rows
// refuse it here as a run-time backstop; the save path refuses it too.
func TestExecuteRefusesAPercentageThresholdWithoutATotal(t *testing.T) {
	t.Parallel()

	customSQL := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT * FROM orders WHERE amount < 0"}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	_, err := NewExecutor().Execute(context.Background(), &execFakeQueryer{},
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, customSQL),
		ExecOptions{})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	// A count rule with a total accepts it: the evaluator divides failed by total.
	nullPercent := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(10), int64(2)),
	}}
	_, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nullPercent),
		ExecOptions{})
	require.NoError(t, err)
}

// The failed-row sample is a second bounded query. Its limit, its SQL and the
// conversion into the proto are pinned here.
func TestExecuteFetchesFailedRowSamples(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	rule := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config)
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(100), int64(5)),
		{Columns: []string{"email"}, Rows: [][]any{{"a@example.com"}, {nil}}},
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL), rule,
		ExecOptions{StoreSamples: true, SampleLimit: 2})
	require.NoError(t, err)
	require.Len(t, queryer.queries, 2)
	require.Contains(t, queryer.queries[1], "`email` IS NULL")
	require.Contains(t, queryer.queries[1], "LIMIT 2")
	require.Equal(t, 2, queryer.options[1].MaxRows)
	require.NotNil(t, execution.Samples)
	require.Len(t, execution.Samples.GetSamples(), 2)
	require.Equal(t, "a@example.com", execution.Samples.GetSamples()[0].GetColumns()[0].GetValue())
	require.Equal(t, "email", execution.Samples.GetSamples()[0].GetColumns()[0].GetColumn())
	// A NULL value has no marker in the proto; the empty string is the contract.
	require.Empty(t, execution.Samples.GetSamples()[1].GetColumns()[0].GetValue())
	require.Equal(t, int64(2), execution.Observation.SampleRows)
	require.False(t, execution.Sampled)
}

// The sample limit follows the workspace rules: unset reads as the default, a
// large value is clamped, and the statement and the query options agree.
func TestExecuteNormalizesTheSampleLimit(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	for _, test := range []struct {
		name      string
		limit     int
		wantLimit string
	}{
		{name: "default", limit: 0, wantLimit: "LIMIT 20"},
		{name: "explicit", limit: 7, wantLimit: "LIMIT 7"},
		{name: "clamped", limit: 500, wantLimit: "LIMIT 100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queryer := &execFakeQueryer{results: []*db.QueryResult{
				aggregateRow([]string{"total_rows", "failed_rows"}, int64(10), int64(3)),
				{Columns: []string{"email"}, Rows: [][]any{{"a@example.com"}}},
			}}
			execution, err := NewExecutor().Execute(context.Background(), queryer,
				execTestResolution(storepb.Engine_MYSQL),
				execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
				ExecOptions{StoreSamples: true, SampleLimit: test.limit})
			require.NoError(t, err)
			require.Contains(t, queryer.queries[1], test.wantLimit)
			require.NotNil(t, execution.Samples)
		})
	}
}

// Sampling is opt-in: without it the executor runs one query and stores no rows.
func TestExecuteSkipsSamplesWhenNotRequested(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(100), int64(5)),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
		ExecOptions{})
	require.NoError(t, err)
	require.Len(t, queryer.queries, 1)
	require.Nil(t, execution.Samples)
	require.Zero(t, execution.Observation.SampleRows)
}

// A passing rule with no failed rows has nothing to sample, and a rule type whose
// failure is a measurement has no failing row at all.
func TestExecuteSkipsSamplesWithoutFailedRows(t *testing.T) {
	t.Parallel()

	nullPercent := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	rowCount := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
	for _, test := range []struct {
		name     string
		ruleType storepb.QualityRuleType
		config   *storepb.QualityRuleConfig
		result   *db.QueryResult
	}{
		{
			name:     "no failed rows",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config:   nullPercent,
			result:   aggregateRow([]string{"total_rows", "failed_rows"}, int64(100), int64(0)),
		},
		{
			name:     "row count has no failing row",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:   rowCount,
			result:   aggregateRow([]string{"total_rows"}, int64(10)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queryer := &execFakeQueryer{results: []*db.QueryResult{test.result}}
			execution, err := NewExecutor().Execute(context.Background(), queryer,
				execTestResolution(storepb.Engine_MYSQL),
				execTestSnapshot(test.ruleType, test.config),
				ExecOptions{StoreSamples: true})
			require.NoError(t, err)
			require.Len(t, queryer.queries, 1, "no sample query is issued")
			require.Nil(t, execution.Samples)
		})
	}
}

// A failed sample query must not turn a good observation into an error: the
// observation is the verdict, the sample is context.
func TestExecuteSwallowsASampleFailure(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	queryer := &execFakeQueryer{
		results: []*db.QueryResult{aggregateRow([]string{"total_rows", "failed_rows"}, int64(100), int64(5))},
		errs:    []error{nil, errors.New("the sample query failed")},
	}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
		ExecOptions{StoreSamples: true})
	require.NoError(t, err)
	require.Nil(t, execution.Samples)
	require.Zero(t, execution.Observation.SampleRows)
	require.Equal(t, int64(5), execution.Observation.FailedRows)
}

// A truncated sample sets the result's truncation flag, because the reviewer is
// looking at a cut-off list rather than all of it.
func TestExecuteCarriesTheSampleTruncation(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(100), int64(5)),
		{Columns: []string{"email"}, Rows: [][]any{{"a@example.com"}}, Truncated: true},
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config),
		ExecOptions{StoreSamples: true})
	require.NoError(t, err)
	require.True(t, execution.Truncated)
	require.True(t, execution.Observation.Truncated)
}

// custom_sql's sample is the user's own query wrapped: DQ never rewrites it.
func TestExecuteSamplesCustomSQLAsItsOwnRows(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
		CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT id, amount FROM orders WHERE amount < 0"},
	}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"failed_rows"}, int64(3)),
		{Columns: []string{"id", "amount"}, Rows: [][]any{{int64(1), float64(-5.5)}}},
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, config),
		ExecOptions{StoreSamples: true})
	require.NoError(t, err)
	require.Contains(t, queryer.queries[1], "SELECT * FROM (SELECT id, amount FROM orders WHERE amount < 0) dq")
	require.NotNil(t, execution.Samples)
	require.Equal(t, "id", execution.Samples.GetSamples()[0].GetColumns()[0].GetColumn())
	require.Equal(t, "1", execution.Samples.GetSamples()[0].GetColumns()[0].GetValue())
	require.Equal(t, "-5.5", execution.Samples.GetSamples()[0].GetColumns()[1].GetValue())
}

// SamplesFromRows is the conversion boundary: every normalized value type has a
// display form, a NULL is an empty string, and a long value is truncated.
func TestSamplesFromRowsFormatsValues(t *testing.T) {
	t.Parallel()

	measured := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	long := strings.Repeat("é", maxSampleValueBytes)
	samples := SamplesFromRows(
		[]string{"text", "count", "ratio", "flag", "when", "missing", "long"},
		[][]any{{"paid", int64(42), 1.5, true, measured, nil, long}},
		0,
	)
	require.Nil(t, samples, "a non-positive limit means no sample")

	samples = SamplesFromRows(
		[]string{"text", "count", "ratio", "flag", "when", "missing", "long"},
		[][]any{{"paid", int64(42), 1.5, true, measured, nil, long}},
		20,
	)
	require.NotNil(t, samples)
	require.Len(t, samples.GetSamples(), 1)
	columns := samples.GetSamples()[0].GetColumns()
	require.Len(t, columns, 7)
	require.Equal(t, "paid", columns[0].GetValue())
	require.Equal(t, "42", columns[1].GetValue())
	require.Equal(t, "1.5", columns[2].GetValue())
	require.Equal(t, "true", columns[3].GetValue())
	require.Equal(t, "2026-10-07T09:30:00Z", columns[4].GetValue())
	require.Empty(t, columns[5].GetValue(), "a NULL value is the empty string")
	require.LessOrEqual(t, len(columns[6].GetValue()), maxSampleValueBytes)
	require.True(t, utf8.ValidString(columns[6].GetValue()))
	require.Equal(t, "long", columns[6].GetColumn())
}

// The conversion keeps the requested limit, clamps it at the maximum, tolerates a
// short row and ignores extra rows.
func TestSamplesFromRowsBoundsTheSample(t *testing.T) {
	t.Parallel()

	rows := make([][]any, 0, 120)
	for index := range 120 {
		rows = append(rows, []any{int64(index)})
	}
	samples := SamplesFromRows([]string{"id"}, rows, 10)
	require.Len(t, samples.GetSamples(), 10)

	samples = SamplesFromRows([]string{"id"}, rows, 1000)
	require.Len(t, samples.GetSamples(), maxSampleLimit)

	// A row with fewer values than columns pads with empty strings rather than
	// dropping the row or panicking.
	samples = SamplesFromRows([]string{"id", "name"}, [][]any{{int64(1)}}, 5)
	require.Len(t, samples.GetSamples(), 1)
	require.Len(t, samples.GetSamples()[0].GetColumns(), 2)
	require.Empty(t, samples.GetSamples()[0].GetColumns()[1].GetValue())

	require.Nil(t, SamplesFromRows(nil, rows, 5))
	require.Nil(t, SamplesFromRows([]string{"id"}, nil, 5))
}

func TestNormalizeSampleLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, defaultSampleLimit, normalizeSampleLimit(0))
	require.Equal(t, defaultSampleLimit, normalizeSampleLimit(-1))
	require.Equal(t, 7, normalizeSampleLimit(7))
	require.Equal(t, maxSampleLimit, normalizeSampleLimit(maxSampleLimit))
	require.Equal(t, maxSampleLimit, normalizeSampleLimit(maxSampleLimit+1))
}

// The recorded SQL is bounded at 8 KiB on a rune boundary, so a stored result
// never holds a truncated invalid UTF-8 string.
func TestTruncateExecutedSQL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "SELECT 1", truncateExecutedSQL("SELECT 1"))

	long := "SELECT '" + strings.Repeat("é", maxExecutedSQLBytes) + "'"
	truncated := truncateExecutedSQL(long)
	require.LessOrEqual(t, len(truncated), maxExecutedSQLBytes)
	require.True(t, utf8.ValidString(truncated))
	require.NotEmpty(t, truncated)
}
