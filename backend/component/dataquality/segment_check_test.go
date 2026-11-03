package dataquality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// segmentCheckConfig builds a segment-check rule config.
func segmentCheckConfig(columns []string, baselineRuns, minOccurrences int32) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SegmentCheck{
		SegmentCheck: &storepb.SegmentCheckConfig{
			Columns:        columns,
			BaselineRuns:   baselineRuns,
			MinOccurrences: minOccurrences,
		},
	}}
}

// segmentRecord is one observed segment in a test baseline.
func segmentRecord(values []string, rowCount int64) *storepb.SegmentRecord {
	return &storepb.SegmentRecord{Values: values, RowCount: rowCount}
}

// TestSegmentCheckSQLGoldens pins the grouped statement: the segment columns and
// the row count in the select list, largest segments first, and one row more than
// the evidence cap so the executor can tell a complete set from a truncated one.
func TestSegmentCheckSQLGoldens(t *testing.T) {
	t.Parallel()

	config := segmentCheckConfig([]string{"status", "amount"}, 0, 0)
	tests := []struct {
		name     string
		engine   storepb.Engine
		filter   Filter
		wantArgs []any
		wantSQL  string
	}{
		{
			name:   "mysql",
			engine: storepb.Engine_MYSQL,
			wantSQL: "SELECT `status`, `amount`, COUNT(*) AS segment_rows FROM `shop`.`orders` " +
				"GROUP BY `status`, `amount` ORDER BY segment_rows DESC, `status`, `amount` LIMIT 501",
		},
		{
			name:   "postgres",
			engine: storepb.Engine_POSTGRES,
			wantSQL: `SELECT "status", "amount", COUNT(*) AS segment_rows FROM "public"."orders" ` +
				`GROUP BY "status", "amount" ORDER BY segment_rows DESC, "status", "amount" LIMIT 501`,
		},
		{
			name:   "mssql",
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT [status], [amount], COUNT(*) AS segment_rows FROM [public].[orders] " +
				"GROUP BY [status], [amount] ORDER BY segment_rows DESC, [status], [amount] " +
				"OFFSET 0 ROWS FETCH NEXT 501 ROWS ONLY",
		},
		{
			name:   "filter arguments",
			engine: storepb.Engine_MYSQL,
			filter: Filter{Clauses: []FilterClause{
				{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
			}},
			wantArgs: []any{"paid"},
			wantSQL: "SELECT `status`, `amount`, COUNT(*) AS segment_rows FROM `shop`.`orders` " +
				"WHERE `status` = ? GROUP BY `status`, `amount` " +
				"ORDER BY segment_rows DESC, `status`, `amount` LIMIT 501",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK,
				Config:  config,
				Filter:  test.filter,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
			require.Equal(t, QuerySample, query.Kind)
			require.Equal(t, []string{"status", "amount", "segment_rows"}, query.Columns)
		})
	}
}

// TestSegmentCheckBoundsAndValidation pins the baseline defaults, the explicit
// ranges and the column rules. Validation, the compiler and the runner's baseline
// read all consult SegmentCheckBounds, so the numbers cannot drift.
func TestSegmentCheckBoundsAndValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		columns        []string
		baselineRuns   int32
		minOccurrences int32
		wantBaseline   int
		wantOccur      int
		wantErr        bool
	}{
		{name: "defaults", columns: []string{"name"}, wantBaseline: 3, wantOccur: 2},
		{name: "one baseline clamps the default occurrence", columns: []string{"name"}, baselineRuns: 1, wantBaseline: 1, wantOccur: 1},
		{name: "explicit window", columns: []string{"name"}, baselineRuns: 5, minOccurrences: 4, wantBaseline: 5, wantOccur: 4},
		{name: "too many baselines", columns: []string{"name"}, baselineRuns: maxSegmentBaselineRuns + 1, wantErr: true},
		{name: "occurrences above the window", columns: []string{"name"}, baselineRuns: 2, minOccurrences: 3, wantErr: true},
		{name: "no columns", columns: nil, wantErr: true},
		{name: "too many columns", columns: []string{"id", "amount", "name", "created_at", "updated_at", "active"}, wantErr: true},
		{name: "unknown column", columns: []string{"missing"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := segmentCheckConfig(test.columns, test.baselineRuns, test.minOccurrences)
			err := ValidateConfig(validationTarget(storepb.Engine_POSTGRES), storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config)
			if test.wantErr {
				require.Error(t, err)
				require.Equal(t, common.Invalid, common.ErrorCode(err), "error: %v", err)
				return
			}
			require.NoError(t, err)
			baselineRuns, occurrences, err := SegmentCheckBounds(config)
			require.NoError(t, err)
			require.Equal(t, test.wantBaseline, baselineRuns)
			require.Equal(t, test.wantOccur, occurrences)
		})
	}
}

// TestSegmentCheckColumnsAreDistinct keeps a duplicated grouping column out of
// the statement: repeating it would change nothing but the SQL text.
func TestSegmentCheckColumnsAreDistinct(t *testing.T) {
	t.Parallel()

	config := segmentCheckConfig([]string{"name", "name"}, 0, 0)
	err := ValidateConfig(validationTarget(storepb.Engine_MYSQL), storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// TestSegmentCheckTemplateRejected pins that a segment check cannot become a
// template: its grouping columns are the target's own columns.
func TestSegmentCheckTemplateRejected(t *testing.T) {
	t.Parallel()

	err := ValidateRuleTemplateConfig(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, segmentCheckConfig([]string{"name"}, 0, 0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "segment-check template")
}

// TestBuildSegmentBaseline pins the expected set: a segment counts when enough of
// the window saw it, the newest observation supplies its last-seen time and row
// count, truncated runs are ignored, and every key a run saw is known so new
// segments can be told apart.
func TestBuildSegmentBaseline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 3, 10, 0, 0, 0, time.UTC)
	runs := []SegmentHistoryRun{
		{
			StartedAt: now,
			Segments: &storepb.SegmentObservation{Observed: []*storepb.SegmentRecord{
				segmentRecord([]string{"open"}, 30),
				segmentRecord([]string{"closed"}, 10),
			}},
		},
		{
			StartedAt: now.Add(-time.Hour),
			Segments: &storepb.SegmentObservation{Observed: []*storepb.SegmentRecord{
				segmentRecord([]string{"open"}, 25),
				segmentRecord([]string{"closed"}, 12),
				segmentRecord([]string{"refunded"}, 1),
			}},
		},
		{
			StartedAt: now.Add(-2 * time.Hour),
			Segments: &storepb.SegmentObservation{
				Truncated: true,
				Observed: []*storepb.SegmentRecord{
					segmentRecord([]string{"open"}, 20),
				},
			},
		},
	}
	baseline := BuildSegmentBaseline(runs, 3, 2)
	require.NotNil(t, baseline)
	// The truncated run is skipped, so two runs form the baseline.
	require.Equal(t, 2, baseline.Runs)
	require.Len(t, baseline.Expected, 2)
	require.Equal(t, []string{"open"}, baseline.Expected[0].Key)
	require.Equal(t, int64(30), baseline.Expected[0].RowCount)
	require.Equal(t, now, baseline.Expected[0].LastSeen)
	require.Equal(t, 2, baseline.Expected[0].Occurrences)
	require.Len(t, baseline.Known, 3)

	// A one-run window still expects what that run saw.
	single := BuildSegmentBaseline(runs[:1], 3, 2)
	require.NotNil(t, single)
	require.Equal(t, 1, single.Runs)
	require.Len(t, single.Expected, 2, "the occurrence threshold is clamped to the available runs")

	require.Nil(t, BuildSegmentBaseline(nil, 3, 2))
}

// TestEvaluateSegmentCheck pins the judgment of the disappeared-segment count:
// a run without a usable baseline is not judged, and count and percent thresholds
// both apply.
func TestEvaluateSegmentCheck(t *testing.T) {
	t.Parallel()

	config := segmentCheckConfig([]string{"status"}, 0, 0)
	noBaseline := Observation{ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, ObservedNull: true}
	state := Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config), noBaseline, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.Status)

	judged := Observation{Observed: 1, FailedRows: 1, TotalRows: 4}
	state = Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config), judged, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.Status, "the default fail threshold is zero")
	require.NotNil(t, state.Thresholds)

	percent := segmentCheckConfig([]string{"status"}, 0, 0)
	percent.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	percent.GetSegmentCheck().FailThreshold = proto.Float64(50)
	state = Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, percent),
		Observation{Observed: 1, FailedRows: 1, TotalRows: 4}, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.Status, "25% is below the 50% failure level")

	warned := segmentCheckConfig([]string{"status"}, 0, 0)
	warned.GetSegmentCheck().FailThreshold = proto.Float64(5)
	warned.GetSegmentCheck().WarnThreshold = proto.Float64(0)
	state = Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, warned),
		Observation{Observed: 1, FailedRows: 1, TotalRows: 4}, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.Status)
}

// TestExecuteSegmentCheckRule pins the executor's segment branch: the first run
// records the set without judging, a later run fails the segments the baseline
// expected, and a truncated observation is never judged.
func TestExecuteSegmentCheckRule(t *testing.T) {
	t.Parallel()

	config := segmentCheckConfig([]string{"status"}, 3, 2)
	snapshot := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config)
	compiled, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    snapshot.Type,
		Config:  config,
	})
	require.NoError(t, err)
	segmentRows := [][]any{{"open", int64(30)}, {"closed", int64(10)}}
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns: compiled.Columns,
		Rows:    segmentRows,
	}}}

	execution, err := executeSegmentCheckRule(context.Background(), queryer, execTestResolution(storepb.Engine_MYSQL), snapshot, ExecOptions{}, compiled, nil)
	require.NoError(t, err)
	require.Equal(t, maxSegmentCount+1, queryer.options[0].MaxRows)
	require.True(t, execution.Observation.ObservedNull, "the first run has no baseline to judge")
	require.False(t, execution.Observation.Truncated)
	require.NotNil(t, execution.Evidence)
	segments := execution.Evidence.GetSegments()
	require.Len(t, segments.GetObserved(), 2)
	require.Equal(t, int64(30), segments.GetObserved()[0].GetRowCount())
	require.Zero(t, segments.GetBaselineRuns())
	require.Empty(t, segments.GetDisappeared())

	// A baseline that expected "open" and "closed" but not "refunded": refunded is
	// new, both expected segments are still present, so nothing disappeared.
	baseline := &SegmentBaseline{
		Runs: 2,
		Expected: []SegmentExpectation{
			{Key: []string{"open"}, RowCount: 20},
			{Key: []string{"closed"}, RowCount: 5},
		},
		Known: map[string]struct{}{
			segmentKey([]string{"open"}, nil):   {},
			segmentKey([]string{"closed"}, nil): {},
		},
	}
	queryer = &execFakeQueryer{results: []*db.QueryResult{{
		Columns: compiled.Columns,
		Rows:    [][]any{{"open", int64(30)}, {"refunded", int64(3)}},
	}}}
	execution, err = executeSegmentCheckRule(context.Background(), queryer, execTestResolution(storepb.Engine_MYSQL), snapshot, ExecOptions{Segments: baseline}, compiled, nil)
	require.NoError(t, err)
	require.False(t, execution.Observation.ObservedNull)
	require.Equal(t, int64(1), execution.Observation.FailedRows, "closed disappeared")
	require.Equal(t, int64(2), execution.Observation.TotalRows)
	segments = execution.Evidence.GetSegments()
	require.Len(t, segments.GetDisappeared(), 1)
	require.Equal(t, []string{"closed"}, segments.GetDisappeared()[0].GetValues())
	require.Equal(t, int64(5), segments.GetDisappeared()[0].GetRowCount())
	require.Len(t, segments.GetNewSegments(), 1, "refunded was never in the baseline")
	require.Equal(t, []string{"refunded"}, segments.GetNewSegments()[0].GetValues())
	require.Equal(t, int32(2), segments.GetBaselineRuns())
	require.Equal(t, int64(2), segments.GetExpectedCount())

	// A truncated observation cannot be compared: a segment beyond the cap would
	// read as missing, so the run is not judged.
	truncatedRows := make([][]any, 0, maxSegmentCount+1)
	for index := range maxSegmentCount + 1 {
		truncatedRows = append(truncatedRows, []any{"segment-" + segmentOrdinal(index), int64(maxSegmentCount - index)})
	}
	queryer = &execFakeQueryer{results: []*db.QueryResult{{
		Columns: compiled.Columns,
		Rows:    truncatedRows,
	}}}
	execution, err = executeSegmentCheckRule(context.Background(), queryer, execTestResolution(storepb.Engine_MYSQL), snapshot, ExecOptions{Segments: baseline}, compiled, nil)
	require.NoError(t, err)
	require.True(t, execution.Truncated)
	require.True(t, execution.Observation.ObservedNull)
	require.Len(t, execution.Evidence.GetSegments().GetObserved(), maxSegmentCount)
}

// segmentOrdinal renders a distinct segment suffix without importing strconv into
// the test's happy path.
func segmentOrdinal(index int) string {
	const digits = "0123456789"
	if index == 0 {
		return "0"
	}
	out := ""
	for index > 0 {
		out = string(digits[index%10]) + out
		index /= 10
	}
	return out
}

// TestSegmentCheckTruncatedBaselineSkipped pins that a truncated evidence row is
// not used as a baseline: its incomplete set would misclassify segments.
func TestSegmentCheckTruncatedBaselineSkipped(t *testing.T) {
	t.Parallel()

	run := SegmentHistoryRun{
		StartedAt: time.Now(),
		Segments: &storepb.SegmentObservation{
			Truncated: true,
			Observed:  []*storepb.SegmentRecord{segmentRecord([]string{"open"}, 1)},
		},
	}
	require.Nil(t, BuildSegmentBaseline([]SegmentHistoryRun{run}, 3, 2))
}

// TestSegmentCheckMasksPersonalSegments pins that a segment key built on a
// personal column is stored as its redacted display form.
func TestSegmentCheckMasksPersonalSegments(t *testing.T) {
	t.Parallel()

	rule := RuleSnapshot{
		Type:   storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK,
		Config: segmentCheckConfig([]string{"id_card"}, 0, 0),
	}
	columns := []string{"id_card"}
	rows := [][]any{{"110101199001011234", int64(2)}}
	records, truncated, err := segmentRecordsFromRows(columns, rows, personalSampleColumns(rule, ExecOptions{MaskColumns: []string{"id_card"}}))
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, records, 1)
	require.True(t, records[0].GetMasked()[0])
	require.NotEqual(t, "110101199001011234", records[0].GetValues()[0])
	require.Contains(t, records[0].GetValues()[0], "1234", "the masked form keeps the last four characters")
}

// TestSegmentCheckEvidenceTimestamp pins that a disappeared segment carries the
// last-seen timestamp it was compared against.
func TestSegmentCheckEvidenceTimestamp(t *testing.T) {
	t.Parallel()

	lastSeen := time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)
	config := segmentCheckConfig([]string{"status"}, 3, 1)
	snapshot := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config)
	compiled, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    snapshot.Type,
		Config:  config,
	})
	require.NoError(t, err)
	baseline := &SegmentBaseline{
		Runs: 1,
		Expected: []SegmentExpectation{
			{Key: []string{"open"}, RowCount: 20, LastSeen: lastSeen, Occurrences: 1},
		},
		Known: map[string]struct{}{segmentKey([]string{"open"}, nil): {}},
	}
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns: compiled.Columns,
		Rows:    [][]any{{"closed", int64(3)}},
	}}}
	execution, err := executeSegmentCheckRule(context.Background(), queryer, execTestResolution(storepb.Engine_MYSQL), snapshot, ExecOptions{Segments: baseline}, compiled, nil)
	require.NoError(t, err)
	disappeared := execution.Evidence.GetSegments().GetDisappeared()
	require.Len(t, disappeared, 1)
	require.Equal(t, lastSeen, disappeared[0].GetLastSeen().AsTime())
	require.True(t, proto.Equal(
		&storepb.SegmentObservation{
			Observed:      []*storepb.SegmentRecord{{Values: []string{"closed"}, IsNull: []bool{false}, Masked: []bool{false}, RowCount: 3}},
			NewSegments:   []*storepb.SegmentRecord{{Values: []string{"closed"}, IsNull: []bool{false}, Masked: []bool{false}, RowCount: 3}},
			Disappeared:   disappeared,
			BaselineRuns:  1,
			ExpectedCount: 1,
		},
		execution.Evidence.GetSegments()))
}

// TestSegmentCheckNULLKeys pins that a NULL grouping value is recorded as NULL and
// compared as one segment rather than as an empty string.
func TestSegmentCheckNULLKeys(t *testing.T) {
	t.Parallel()

	records, _, err := segmentRecordsFromRows([]string{"status"}, [][]any{{nil, int64(4)}}, nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, []bool{true}, records[0].GetIsNull())
	require.Equal(t, []string{""}, records[0].GetValues())

	baseline := &SegmentBaseline{
		Runs:     1,
		Expected: []SegmentExpectation{{Key: []string{""}, IsNull: []bool{true}, RowCount: 4}},
		Known:    map[string]struct{}{segmentKey([]string{""}, []bool{true}): {}},
	}
	config := segmentCheckConfig([]string{"status"}, 3, 1)
	snapshot := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK, config)
	compiled, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    snapshot.Type,
		Config:  config,
	})
	require.NoError(t, err)
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns: compiled.Columns,
		Rows:    [][]any{{nil, int64(4)}},
	}}}
	execution, err := executeSegmentCheckRule(context.Background(), queryer, execTestResolution(storepb.Engine_MYSQL), snapshot, ExecOptions{Segments: baseline}, compiled, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), execution.Observation.FailedRows, "the NULL segment is still present")
}
