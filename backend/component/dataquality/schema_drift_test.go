package dataquality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// schemaDriftSnapshot builds a drift snapshot from a resolved target's columns.
func schemaDriftSnapshot(columns []ColumnInfo) []*storepb.SchemaDriftColumn {
	snapshot := make([]*storepb.SchemaDriftColumn, 0, len(columns))
	for _, column := range columns {
		snapshot = append(snapshot, &storepb.SchemaDriftColumn{
			Name:     column.Name,
			Type:     column.Type,
			Nullable: column.Nullable,
		})
	}
	return snapshot
}

// schemaDriftConfig builds a drift rule config with the given snapshot.
func schemaDriftConfig(expected []*storepb.SchemaDriftColumn) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{
		SchemaDrift: &storepb.SchemaDriftConfig{ExpectedColumns: expected},
	}}
}

// TestCompileSchemaDriftEmitsNoSQL pins the metadata-rule contract: the compiler
// returns an empty statement and the QueryMetadata kind so no executor runs SQL
// against the target and the preview has nothing to show.
func TestCompileSchemaDriftEmitsNoSQL(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	config := schemaDriftConfig(schemaDriftSnapshot(target.Columns))
	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		dialect, err := DialectFor(engine)
		require.NoError(t, err)
		query, err := Compile(CompileInput{
			Dialect: dialect,
			Target:  compileTestTarget(engine),
			Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
			Config:  config,
		})
		require.NoError(t, err)
		require.Equal(t, QueryMetadata, query.Kind)
		require.Empty(t, query.SQL)
		require.Empty(t, query.Columns)
		require.Empty(t, query.Args)
		// A dry run has nothing to probe for a metadata rule.
		require.Empty(t, ProbeStatement(engine, query))
	}

	// The sample compiler has nothing to query for a metadata rule either.
	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	sample, err := CompileFailureRows(CompileInput{
		Dialect: dialect,
		Target:  target,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		Config:  config,
	}, 20)
	require.NoError(t, err)
	require.Empty(t, sample.SQL)
}

// TestCompareSchemaDrift pins the comparison semantics: missing columns, changed
// types and nullability (check_types), changed order among shared columns
// (check_order) and extra columns (allow_extra_columns), with stable item order.
func TestCompareSchemaDrift(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	snapshot := schemaDriftSnapshot(target.Columns)

	tests := []struct {
		name    string
		current []ColumnInfo
		mutate  func(*storepb.SchemaDriftConfig)
		want    []schemaDriftItem
	}{
		{
			name:    "no drift",
			current: target.Columns,
			want:    nil,
		},
		{
			name:    "missing column",
			current: withoutColumn(target.Columns, "email"),
			want:    []schemaDriftItem{{Column: "email", Reason: "missing"}},
		},
		{
			name:    "extra column",
			current: append(append([]ColumnInfo{}, target.Columns...), ColumnInfo{Name: "surprise", Type: "text", Kind: ColumnKindString}),
			want:    []schemaDriftItem{{Column: "surprise", Reason: "extra"}},
		},
		{
			name:    "extra column allowed",
			current: append(append([]ColumnInfo{}, target.Columns...), ColumnInfo{Name: "surprise", Type: "text", Kind: ColumnKindString}),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.AllowExtraColumns = true
			},
			want: nil,
		},
		{
			name:    "type changed",
			current: replaceColumnType(target.Columns, "email", "text"),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.CheckTypes = proto.Bool(true)
			},
			want: []schemaDriftItem{{Column: "email", Reason: "type: varchar(255) -> text"}},
		},
		{
			name:    "type changed but check_types off",
			current: replaceColumnType(target.Columns, "email", "text"),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.CheckTypes = proto.Bool(false)
			},
			want: nil,
		},
		{
			name:    "order changed",
			current: swapColumns(target.Columns, 0, 1),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.CheckOrder = true
			},
			want: []schemaDriftItem{
				{Column: "id", Reason: "order"},
				{Column: "email", Reason: "order"},
			},
		},
		{
			name:    "order changed but check_order off",
			current: swapColumns(target.Columns, 0, 1),
			want:    nil,
		},
		{
			name:    "order ignores missing and extra columns",
			current: append(withoutColumn(target.Columns, "email"), ColumnInfo{Name: "surprise", Type: "text", Kind: ColumnKindString}),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.CheckOrder = true
			},
			want: []schemaDriftItem{
				{Column: "email", Reason: "missing"},
				{Column: "surprise", Reason: "extra"},
			},
		},
		{
			name:    "all checks together",
			current: append(withoutColumn(target.Columns, "email"), ColumnInfo{Name: "surprise", Type: "text", Kind: ColumnKindString}),
			mutate: func(config *storepb.SchemaDriftConfig) {
				config.CheckTypes = proto.Bool(true)
				config.CheckOrder = true
			},
			want: []schemaDriftItem{
				{Column: "email", Reason: "missing"},
				{Column: "surprise", Reason: "extra"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := &storepb.SchemaDriftConfig{ExpectedColumns: snapshot}
			if test.mutate != nil {
				test.mutate(config)
			}
			require.Equal(t, test.want, compareSchemaDrift(test.current, config))
		})
	}
}

// withoutColumn returns a copy of the column list without the named column.
//
//nolint:unparam // the fixtures drop one column today, but the helper stays general.
func withoutColumn(columns []ColumnInfo, name string) []ColumnInfo {
	result := make([]ColumnInfo, 0, len(columns))
	for _, column := range columns {
		if column.Name == name {
			continue
		}
		result = append(result, column)
	}
	return result
}

// replaceColumnType returns a copy of the column list with one type changed.
func replaceColumnType(columns []ColumnInfo, name, columnType string) []ColumnInfo {
	result := append([]ColumnInfo{}, columns...)
	for index := range result {
		if result[index].Name == name {
			result[index].Type = columnType
		}
	}
	return result
}

// swapColumns returns a copy of the column list with two positions exchanged.
func swapColumns(columns []ColumnInfo, left, right int) []ColumnInfo {
	result := append([]ColumnInfo{}, columns...)
	if left >= 0 && right >= 0 && left < len(result) && right < len(result) {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

// setColumnNullable returns a copy of the column list with one column's
// nullability changed.
func setColumnNullable(columns []ColumnInfo, name string, nullable bool) []ColumnInfo {
	result := append([]ColumnInfo{}, columns...)
	for index := range result {
		if result[index].Name == name {
			result[index].Nullable = nullable
		}
	}
	return result
}

// TestSchemaDriftNullability pins nullability as part of the column contract: a
// column that gained or lost NOT NULL is drift under check_types, the check can be
// switched off, and an equal snapshot reports nothing.
func TestSchemaDriftNullability(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	tests := []struct {
		name             string
		snapshotNullable bool
		currentNullable  bool
		checkTypes       *bool
		want             []schemaDriftItem
	}{
		{
			name:             "gained null",
			snapshotNullable: false,
			currentNullable:  true,
			checkTypes:       proto.Bool(true),
			want:             []schemaDriftItem{{Column: "email", Reason: "nullable: NOT NULL -> NULL"}},
		},
		{
			name:             "lost null",
			snapshotNullable: true,
			currentNullable:  false,
			checkTypes:       proto.Bool(true),
			want:             []schemaDriftItem{{Column: "email", Reason: "nullable: NULL -> NOT NULL"}},
		},
		{
			name:             "unchanged",
			snapshotNullable: true,
			currentNullable:  true,
			checkTypes:       proto.Bool(true),
			want:             nil,
		},
		{
			name:             "check_types off",
			snapshotNullable: false,
			currentNullable:  true,
			checkTypes:       proto.Bool(false),
			want:             nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := schemaDriftSnapshot(setColumnNullable(target.Columns, "email", test.snapshotNullable))
			config := &storepb.SchemaDriftConfig{ExpectedColumns: snapshot, CheckTypes: test.checkTypes}
			current := setColumnNullable(target.Columns, "email", test.currentNullable)
			require.Equal(t, test.want, compareSchemaDrift(current, config))
		})
	}
}

// TestSchemaDriftSamples pins the sample shape: one row per drift item, the column
// in the column field and the reason in the value field, capped by the limit.
func TestSchemaDriftSamples(t *testing.T) {
	t.Parallel()

	items := []schemaDriftItem{
		{Column: "email", Reason: "missing"},
		{Column: "amount", Reason: "type: decimal(10,2) -> text"},
		{Column: "surprise", Reason: "extra"},
	}
	samples := schemaDriftSamples(items, 2)
	require.NotNil(t, samples)
	require.Len(t, samples.GetSamples(), 2)
	require.Equal(t, "email", samples.GetSamples()[0].GetColumns()[0].GetColumn())
	require.Equal(t, "missing", samples.GetSamples()[0].GetColumns()[0].GetValue())
	require.Equal(t, "amount", samples.GetSamples()[1].GetColumns()[0].GetColumn())

	require.Nil(t, schemaDriftSamples(nil, 20))
	require.Nil(t, schemaDriftSamples(items, 0))
}

// TestExecuteSchemaDriftWithoutQueryer pins that a metadata rule runs with no
// read-only data source: it records no SQL, reports the drift count out of the
// snapshot and builds its sample from the comparison.
func TestExecuteSchemaDriftWithoutQueryer(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	snapshot := schemaDriftSnapshot(target.Columns)
	// Drop one expected column and add one live column: two drift items.
	current := append(withoutColumn(target.Columns, "email"), ColumnInfo{Name: "surprise", Type: "text", Kind: ColumnKindString})
	resolution := &Resolution{Target: target}
	resolution.Target.Columns = current

	snapshotConfig := schemaDriftConfig(snapshot)
	execution, err := NewExecutor().Execute(context.Background(), nil, resolution,
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, snapshotConfig),
		ExecOptions{StoreSamples: true, SampleLimit: 1})
	require.NoError(t, err)
	require.Empty(t, execution.ExecutedSQL)
	require.Equal(t, int64(len(snapshot)), execution.Observation.TotalRows)
	require.Equal(t, int64(2), execution.Observation.FailedRows)
	require.Equal(t, float64(2), execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, execution.Observation.ObservedKind)
	// The sample is capped at one row, and the execution says it was truncated.
	require.Len(t, execution.Samples.GetSamples(), 1)
	require.True(t, execution.Truncated)
	require.Equal(t, int64(1), execution.Observation.SampleRows)

	execution, err = NewExecutor().Execute(context.Background(), nil, resolution,
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, snapshotConfig),
		ExecOptions{})
	require.NoError(t, err)
	require.Nil(t, execution.Samples)
	require.False(t, execution.Truncated)
}

// TestExecuteNonMetadataRuleNeedsQueryer keeps the metadata exemption narrow: every
// other rule type still requires the read-only queryer.
func TestExecuteNonMetadataRuleNeedsQueryer(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{
		GroupCheck: &storepb.GroupCheckConfig{
			Columns:         []string{"status"},
			MinRowsPerGroup: int64Ptr(1),
		},
	}}
	_, err := NewExecutor().Execute(context.Background(), nil, execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, config), ExecOptions{})
	require.Error(t, err)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}

// TestEvaluateSchemaDrift pins the count-threshold judgement and the on-empty
// irrelevance: a drift snapshot always has rows to compare.
func TestEvaluateSchemaDrift(t *testing.T) {
	t.Parallel()

	failOnly := schemaDriftConfig(nil)
	failOnly.GetSchemaDrift().FailThreshold = proto.Float64(2)
	failRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, Config: failOnly}

	state := Evaluate(failRule, Observation{TotalRows: 5, FailedRows: 0}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	// The comparison is strict: a value equal to the fail threshold passes it.
	state = Evaluate(failRule, Observation{TotalRows: 5, FailedRows: 2}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(failRule, Observation{TotalRows: 5, FailedRows: 3}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, state.GetThresholds().GetComparison())

	warnAndFail := schemaDriftConfig(nil)
	warnAndFail.GetSchemaDrift().FailThreshold = proto.Float64(3)
	warnAndFail.GetSchemaDrift().WarnThreshold = proto.Float64(1)
	warnRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, Config: warnAndFail}
	state = Evaluate(warnRule, Observation{TotalRows: 5, FailedRows: 2}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.GetStatus())
}

// TestValidateSchemaDriftConfig pins every save-time refusal of a drift rule.
func TestValidateSchemaDriftConfig(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	valid := schemaDriftSnapshot(target.Columns)
	mutate := func(config *storepb.SchemaDriftConfig) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: config}}
	}

	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		mutate(&storepb.SchemaDriftConfig{ExpectedColumns: valid})))

	tests := []struct {
		name        string
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:        "missing config",
			config:      &storepb.QualityRuleConfig{},
			wantMessage: "empty",
		},
		{
			name:        "no expected columns",
			config:      mutate(&storepb.SchemaDriftConfig{}),
			wantMessage: "at least one expected column",
		},
		{
			name: "empty name",
			config: mutate(&storepb.SchemaDriftConfig{ExpectedColumns: []*storepb.SchemaDriftColumn{
				{Name: " ", Type: "bigint"},
			}}),
			wantMessage: "needs a name",
		},
		{
			name: "duplicate name",
			config: mutate(&storepb.SchemaDriftConfig{ExpectedColumns: []*storepb.SchemaDriftColumn{
				{Name: "id", Type: "bigint"},
				{Name: "id", Type: "int"},
			}}),
			wantMessage: "more than once",
		},
		{
			name: "empty type",
			config: mutate(&storepb.SchemaDriftConfig{ExpectedColumns: []*storepb.SchemaDriftColumn{
				{Name: "id"},
			}}),
			wantMessage: "needs a type",
		},
		{
			name: "percent metric",
			config: func() *storepb.QualityRuleConfig {
				config := mutate(&storepb.SchemaDriftConfig{ExpectedColumns: valid})
				config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
				return config
			}(),
			wantMessage: "PERCENT is not supported",
		},
		{
			name: "negative fail threshold",
			config: mutate(&storepb.SchemaDriftConfig{
				ExpectedColumns: valid,
				FailThreshold:   proto.Float64(-1),
			}),
			wantMessage: "must not be negative",
		},
		{
			name: "warn above fail",
			config: mutate(&storepb.SchemaDriftConfig{
				ExpectedColumns: valid,
				FailThreshold:   proto.Float64(1),
				WarnThreshold:   proto.Float64(2),
			}),
			wantMessage: "exceeds failure threshold",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestSchemaDriftSupport pins the capability table: the metadata rule works on
// every dialect (it runs no SQL, MSSQL included) and watches any relation type
// that has columns.
func TestSchemaDriftSupport(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT)
		require.True(t, ok, reason)
	}
	for _, metaType := range []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
	} {
		require.True(t, RuleTypeForTarget(metaType, storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT), metaType.String())
	}
	require.True(t, IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT))
	require.False(t, IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK))
}
