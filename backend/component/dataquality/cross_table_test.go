package dataquality

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// crossTableConfig builds a reconciliation rule config.
func crossTableConfig(rightTable string, left storepb.QualityAggregateFunction, leftColumn string,
	right storepb.QualityAggregateFunction, rightColumn string, tolerance *float64) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{
		CrossTable: &storepb.CrossTableConfig{
			RightTable:    rightTable,
			LeftFunction:  left,
			LeftColumn:    leftColumn,
			RightFunction: right,
			RightColumn:   rightColumn,
			Tolerance:     tolerance,
		},
	}}
}

// TestCrossTableSQLGoldens pins the reconciliation statement on every dialect
// family: two scalar subqueries in one row, the filter on the left side only, and
// the right table resolved into the target's own container.
func TestCrossTableSQLGoldens(t *testing.T) {
	t.Parallel()

	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	tests := []struct {
		name     string
		config   *storepb.QualityRuleConfig
		filter   Filter
		mysqlSQL string
		pgSQL    string
		mssqlSQL string
		wantArgs []any
	}{
		{
			name: "count both sides",
			config: crossTableConfig("customers",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			mysqlSQL: "SELECT (SELECT COUNT(*) FROM `shop`.`orders`) AS left_value, " +
				"(SELECT COUNT(*) FROM `shop`.`customers`) AS right_value",
			pgSQL: `SELECT (SELECT COUNT(*) FROM "public"."orders") AS left_value, ` +
				`(SELECT COUNT(*) FROM "public"."customers") AS right_value`,
			mssqlSQL: "SELECT (SELECT COUNT(*) FROM [public].[orders]) AS left_value, " +
				"(SELECT COUNT(*) FROM [public].[customers]) AS right_value",
		},
		{
			name: "sum both sides",
			config: crossTableConfig("customers",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "total", proto.Float64(5)),
			mysqlSQL: "SELECT (SELECT SUM(`amount`) FROM `shop`.`orders`) AS left_value, " +
				"(SELECT SUM(`total`) FROM `shop`.`customers`) AS right_value",
			pgSQL: `SELECT (SELECT SUM("amount") FROM "public"."orders") AS left_value, ` +
				`(SELECT SUM("total") FROM "public"."customers") AS right_value`,
			mssqlSQL: "SELECT (SELECT SUM([amount]) FROM [public].[orders]) AS left_value, " +
				"(SELECT SUM([total]) FROM [public].[customers]) AS right_value",
		},
		{
			name: "filter applies to the left side only",
			config: crossTableConfig("customers",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, "total", nil),
			filter: filter,
			mysqlSQL: "SELECT (SELECT COUNT(*) FROM `shop`.`orders` WHERE `status` = ?) AS left_value, " +
				"(SELECT AVG(`total`) FROM `shop`.`customers`) AS right_value",
			pgSQL: `SELECT (SELECT COUNT(*) FROM "public"."orders" WHERE "status" = $1) AS left_value, ` +
				`(SELECT AVG("total") FROM "public"."customers") AS right_value`,
			mssqlSQL: "SELECT (SELECT COUNT(*) FROM [public].[orders] WHERE [status] = @p1) AS left_value, " +
				"(SELECT AVG([total]) FROM [public].[customers]) AS right_value",
			wantArgs: []any{"paid"},
		},
	}
	for _, test := range tests {
		for _, engine := range []storepb.Engine{
			storepb.Engine_MYSQL, storepb.Engine_STARROCKS, storepb.Engine_POSTGRES, storepb.Engine_MSSQL,
		} {
			t.Run(test.name+"/"+engine.String(), func(t *testing.T) {
				t.Parallel()
				dialect, err := DialectFor(engine)
				require.NoError(t, err)
				query, err := Compile(CompileInput{
					Dialect: dialect,
					Target:  compileTestTarget(engine),
					Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
					Config:  test.config,
					Filter:  test.filter,
				})
				require.NoError(t, err)
				wantSQL := test.mysqlSQL
				switch engine {
				case storepb.Engine_POSTGRES:
					wantSQL = test.pgSQL
				case storepb.Engine_MSSQL:
					wantSQL = test.mssqlSQL
				default:
					// The MySQL family (MySQL, StarRocks) shares one rendering.
				}
				require.Equal(t, wantSQL, query.SQL)
				require.Equal(t, test.wantArgs, query.Args)
				require.Equal(t, []string{"left_value", "right_value"}, query.Columns)
				require.Equal(t, QueryAggregate, query.Kind)
			})
		}
	}
}

// TestCrossTableRightTableRules pins the same-container rule: one segment takes the
// target's database (MySQL family) or schema (PostgreSQL/MSSQL); two segments must
// name the target's own container.
func TestCrossTableRightTableRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		engine      storepb.Engine
		rightTable  string
		wantSQLPart string
		wantMessage string
	}{
		{
			name:        "mysql one segment",
			engine:      storepb.Engine_MYSQL,
			rightTable:  "customers",
			wantSQLPart: "FROM `shop`.`customers`",
		},
		{
			name:        "mysql same database",
			engine:      storepb.Engine_MYSQL,
			rightTable:  "shop.customers",
			wantSQLPart: "FROM `shop`.`customers`",
		},
		{
			name:        "mysql other database",
			engine:      storepb.Engine_MYSQL,
			rightTable:  "other.customers",
			wantMessage: "must be in the rule's database",
		},
		{
			name:        "mysql too many segments",
			engine:      storepb.Engine_MYSQL,
			rightTable:  "a.b.c",
			wantMessage: "must be \"table\" or \"database.table\"",
		},
		{
			name:        "postgres one segment",
			engine:      storepb.Engine_POSTGRES,
			rightTable:  "customers",
			wantSQLPart: `FROM "public"."customers"`,
		},
		{
			name:        "postgres same schema",
			engine:      storepb.Engine_POSTGRES,
			rightTable:  "public.customers",
			wantSQLPart: `FROM "public"."customers"`,
		},
		{
			name:        "postgres other schema",
			engine:      storepb.Engine_POSTGRES,
			rightTable:  "other.customers",
			wantMessage: "must be in the rule's schema",
		},
		{
			name:        "empty right table",
			engine:      storepb.Engine_POSTGRES,
			rightTable:  " ",
			wantMessage: "needs a right table",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			config := crossTableConfig(test.rightTable,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
				Config:  config,
			})
			if test.wantMessage != "" {
				require.Error(t, err)
				require.Equal(t, common.Invalid, common.ErrorCode(err))
				require.Contains(t, err.Error(), test.wantMessage)
				return
			}
			require.NoError(t, err)
			require.Contains(t, query.SQL, test.wantSQLPart)
		})
	}
}

// TestEvaluateCrossTable pins the tolerance judgement: the tolerance is the GT
// threshold on the absolute difference, so a difference equal to it passes.
func TestEvaluateCrossTable(t *testing.T) {
	t.Parallel()

	exact := crossTableConfig("customers",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
	exactRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: exact}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(exactRule, Observation{Observed: 0}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(exactRule, Observation{Observed: 0.5}, testNow).GetStatus())

	tolerated := crossTableConfig("customers",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(5))
	toleratedRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: tolerated}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(toleratedRule, Observation{Observed: 5}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(toleratedRule, Observation{Observed: 5.1}, testNow).GetStatus())

	state := Evaluate(toleratedRule, Observation{Observed: 5}, testNow)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, state.GetThresholds().GetComparison())
	require.Equal(t, 0.0, state.GetThresholds().GetFailValue(), "the tolerance is absorbed before the default fail threshold of zero")

	// An explicit fail/warn pair bounds the part of the difference beyond the
	// tolerance: excess 1.5 warns above 1 and fails only above 2.
	thresholded := crossTableConfig("customers",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(5))
	thresholded.GetCrossTable().FailThreshold = proto.Float64(2)
	thresholded.GetCrossTable().WarnThreshold = proto.Float64(1)
	thresholdedRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: thresholded}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(thresholdedRule, Observation{Observed: 6}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, Evaluate(thresholdedRule, Observation{Observed: 6.5}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(thresholdedRule, Observation{Observed: 7.5}, testNow).GetStatus())
	require.Equal(t, 2.0, Evaluate(thresholdedRule, Observation{Observed: 7.5}, testNow).GetThresholds().GetFailValue())
	require.Equal(t, 1.0, Evaluate(thresholdedRule, Observation{Observed: 7.5}, testNow).GetThresholds().GetWarnValue())

	// A NULL on either side is missing data, decided by on_empty.
	nullObservation := Observation{ObservedNull: true}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, Evaluate(toleratedRule, nullObservation, testNow).GetStatus())
	tolerated.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(toleratedRule, nullObservation, testNow).GetStatus())
}

// TestExecuteCrossTableObservation pins the executor's interpretation: the
// absolute difference is a NUMBER, and a NULL on either side sets ObservedNull.
func TestExecuteCrossTableObservation(t *testing.T) {
	t.Parallel()

	config := crossTableConfig("customers",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "total", proto.Float64(1))
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, int64(100), int64(90)),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config), ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, 10.0, execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
	require.False(t, execution.Observation.ObservedNull)

	// Both sides are numeric, but SQL SUM can normalize to float64.
	queryer = &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, 100.5, 90.0),
	}}
	execution, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config), ExecOptions{})
	require.NoError(t, err)
	require.InDelta(t, 10.5, execution.Observation.Observed, 1e-9)

	queryer = &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, nil, int64(90)),
	}}
	execution, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config), ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Observation.ObservedNull)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
}

// TestValidateCrossTableConfig pins every save-time refusal of a reconciliation
// rule.
func TestValidateCrossTableConfig(t *testing.T) {
	t.Parallel()

	pgTarget := compileTestTarget(storepb.Engine_POSTGRES)
	mysqlTarget := compileTestTarget(storepb.Engine_MYSQL)
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)))
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		crossTableConfig("public.customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, "total", proto.Float64(1))))
	// The evaluator subtracts the two observations as numbers, so a temporal MIN/MAX
	// is not a difference and is refused even against a COUNT right side.
	err := ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, "created_at",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "needs a numeric column")

	tests := []struct {
		name        string
		target      ResolvedTarget
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:        "missing config",
			target:      pgTarget,
			config:      &storepb.QualityRuleConfig{},
			wantMessage: "empty",
		},
		{
			name:   "empty right table",
			target: pgTarget,
			config: crossTableConfig("", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "needs a right table",
		},
		{
			name:   "right table in another database",
			target: mysqlTarget,
			config: crossTableConfig("other.customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "must be in the rule's database",
		},
		{
			name:   "unspecified left function",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "cross-table function is required",
		},
		{
			name:   "unsupported right function",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV, "amount", nil),
			wantMessage: "cross_table supports COUNT, SUM, AVG, MIN and MAX only",
		},
		{
			name:   "count with a left column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "amount",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "takes no left_column",
		},
		{
			name:   "count with a right column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "total", nil),
			wantMessage: "takes no right_column",
		},
		{
			name:   "sum without a left column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "column is required",
		},
		{
			name:   "unknown left column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "nope",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "does not exist",
		},
		{
			name:   "sum on a string left column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "status",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "needs a numeric column",
		},
		{
			name:   "non-count without a right column",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "", nil),
			wantMessage: "needs a right_column",
		},
		{
			name:   "negative tolerance",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(-1)),
			wantMessage: "non-negative",
		},
		{
			name:   "non-finite tolerance",
			target: pgTarget,
			config: crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(math.NaN())),
			wantMessage: "non-negative",
		},
		{
			name:   "percent metric",
			target: pgTarget,
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
				config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
				return config
			}(),
			wantMessage: "absolute values",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfig(test.target, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestCrossTableSupport pins the capability table: every dialect can reconcile,
// but only tables (a view has no separate registry column authority).
func TestCrossTableSupport(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE)
		require.True(t, ok, reason)
	}
	require.True(t, RuleTypeForTarget(storepb.MetaType_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE))
	require.True(t, RuleTypeForTarget(storepb.MetaType_EXTERNAL_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE))
	require.False(t, RuleTypeForTarget(storepb.MetaType_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE))
	require.False(t, RuleTypeForTarget(storepb.MetaType_MATERIALIZED_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE))
}

// TestCrossTableExecutionRejectsPercentMetric pins the runtime backstop: a
// tolerance is an absolute value, so a PERCENT metric is refused before any query
// runs.
func TestCrossTableExecutionRejectsPercentMetric(t *testing.T) {
	t.Parallel()

	config := crossTableConfig("customers", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	_, err := NewExecutor().Execute(context.Background(), nil, execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config), ExecOptions{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "absolute values")
}
