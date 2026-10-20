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

// aggregateConfig builds an aggregate rule config with a fail interval.
func aggregateConfig(column string, function storepb.QualityAggregateFunction, minValue, maxValue *float64) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Aggregate{
		Aggregate: &storepb.AggregateConfig{
			Column:   column,
			Function: function,
			Min:      minValue,
			Max:      maxValue,
		},
	}}
}

// TestAggregateSQLGoldens pins the aggregate statement on every dialect family:
// COUNT(*) ignores the column, arithmetic/min/max aggregate it, and the engine
// spellings differ only for standard deviation.
func TestAggregateSQLGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *storepb.QualityRuleConfig
		mysqlSQL string
		pgSQL    string
		mssqlSQL string
	}{
		{
			name:     "count",
			config:   aggregateConfig("id", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, proto.Float64(1), nil),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, COUNT(*) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, COUNT(*) AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, COUNT(*) AS observed FROM [public].[orders]",
		},
		{
			name:     "sum",
			config:   aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), proto.Float64(100)),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(`amount`) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, SUM("amount") AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM([amount]) AS observed FROM [public].[orders]",
		},
		{
			name:     "avg",
			config:   aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, proto.Float64(0), nil),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, AVG(`amount`) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, AVG("amount") AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, AVG([amount]) AS observed FROM [public].[orders]",
		},
		{
			name:     "min on a temporal column",
			config:   aggregateConfig("created_at", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, proto.Float64(0), nil),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, MIN(`created_at`) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, MIN("created_at") AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, MIN([created_at]) AS observed FROM [public].[orders]",
		},
		{
			name:     "max",
			config:   aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX, nil, proto.Float64(1000)),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, MAX(`amount`) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, MAX("amount") AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, MAX([amount]) AS observed FROM [public].[orders]",
		},
		{
			name:     "stddev",
			config:   aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV, proto.Float64(0), proto.Float64(10)),
			mysqlSQL: "SELECT COUNT(*) AS total_rows, STDDEV(`amount`) AS observed FROM `shop`.`orders`",
			pgSQL:    `SELECT COUNT(*) AS total_rows, STDDEV("amount") AS observed FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, STDEV([amount]) AS observed FROM [public].[orders]",
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
					Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
					Config:  test.config,
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
				require.Empty(t, query.Args)
				require.Equal(t, []string{"total_rows", "observed"}, query.Columns)
				require.Equal(t, QueryAggregate, query.Kind)
			})
		}
	}
}

// TestAggregatePercentileSQL pins the percentile-cont statement: the percentile is
// bound as a parameter (0–100 → 0–1 fraction) and T-SQL needs OVER ().
func TestAggregatePercentileSQL(t *testing.T) {
	t.Parallel()

	config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, proto.Float64(0), proto.Float64(100))
	config.GetAggregate().Percentile = proto.Float64(50)
	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}

	// PostgreSQL without a filter: the percentile is the first argument.
	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		Config:  config,
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, PERCENTILE_CONT($1) WITHIN GROUP (ORDER BY "amount") `+
		`AS observed FROM "public"."orders"`, query.SQL)
	require.Equal(t, []any{0.5}, query.Args)

	// With a filter, the filter keeps its numbering and the percentile is numbered
	// after it.
	query, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		Config:  config,
		Filter:  filter,
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT COUNT(*) AS total_rows, PERCENTILE_CONT($2) WITHIN GROUP (ORDER BY "amount") `+
		`AS observed FROM "public"."orders" WHERE "status" = $1`, query.SQL)
	require.Equal(t, []any{"paid", 0.5}, query.Args)

	// SQL Server wraps the ordered set with OVER () and uses @pN placeholders.
	dialect, err = DialectFor(storepb.Engine_MSSQL)
	require.NoError(t, err)
	query, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MSSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		Config:  config,
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT COUNT(*) AS total_rows, PERCENTILE_CONT(@p1) WITHIN GROUP (ORDER BY [amount]) "+
		"OVER () AS observed FROM [public].[orders]", query.SQL)
	require.Equal(t, []any{0.5}, query.Args)

	// The MySQL family and StarRocks have no percentile-cont aggregate: the compiler
	// refuses it and validation reports the engine restriction.
	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_STARROCKS} {
		dialect, err := DialectFor(engine)
		require.NoError(t, err)
		_, err = Compile(CompileInput{
			Dialect: dialect,
			Target:  compileTestTarget(engine),
			Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			Config:  config,
		})
		require.Equal(t, common.Invalid, common.ErrorCode(err))

		err = ValidateConfig(compileTestTarget(engine), storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, config)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Contains(t, err.Error(), "PERCENTILE is only supported")
	}
}

// TestEvaluateAggregateInterval pins the interval judgement: open bounds fail on
// their endpoint, inclusive bounds allow it, and a warn bound narrows the pass
// range.
func TestEvaluateAggregateInterval(t *testing.T) {
	t.Parallel()

	open := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(10), proto.Float64(100))
	openRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, Config: open}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(openRule, Observation{Observed: 50}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(openRule, Observation{Observed: 10}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(openRule, Observation{Observed: 100}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(openRule, Observation{Observed: 9.9}, testNow).GetStatus())

	inclusive := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(10), proto.Float64(100))
	inclusive.GetAggregate().Inclusive = true
	inclusiveRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, Config: inclusive}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(inclusiveRule, Observation{Observed: 10}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(inclusiveRule, Observation{Observed: 100}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(inclusiveRule, Observation{Observed: 101}, testNow).GetStatus())

	warned := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), proto.Float64(100))
	warned.GetAggregate().WarnMin = proto.Float64(20)
	warned.GetAggregate().WarnMax = proto.Float64(80)
	warnedRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, Config: warned}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(warnedRule, Observation{Observed: 50}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, Evaluate(warnedRule, Observation{Observed: 15}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, Evaluate(warnedRule, Observation{Observed: 90}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(warnedRule, Observation{Observed: -1}, testNow).GetStatus())
}

// TestEvaluateAggregateNullIsEmptyData pins that a NULL aggregate follows on_empty
// instead of reading as zero.
func TestEvaluateAggregateNullIsEmptyData(t *testing.T) {
	t.Parallel()

	config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil)
	rule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, Config: config}
	nullObservation := Observation{ObservedNull: true, TotalRows: 5}

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, Evaluate(rule, nullObservation, testNow).GetStatus())
	config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(rule, nullObservation, testNow).GetStatus())
	config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_FAIL
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(rule, nullObservation, testNow).GetStatus())
}

// TestExecuteAggregateObservation pins the executor's interpretation: a real value
// is a NUMBER, a NULL aggregate sets ObservedNull, and total_rows is carried.
func TestExecuteAggregateObservation(t *testing.T) {
	t.Parallel()

	config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, proto.Float64(0), nil)
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "observed"}, int64(100), 12.5),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, config), ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(100), execution.Observation.TotalRows)
	require.Equal(t, 12.5, execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
	require.False(t, execution.Observation.ObservedNull)

	queryer = &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "observed"}, int64(0), nil),
	}}
	execution, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, config), ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Observation.ObservedNull)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
}

// TestValidateAggregateConfig pins every save-time refusal of an aggregate rule.
func TestValidateAggregateConfig(t *testing.T) {
	t.Parallel()

	pgTarget := compileTestTarget(storepb.Engine_POSTGRES)
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), proto.Float64(100))))

	percentileConfig := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, proto.Float64(0), proto.Float64(100))
	percentileConfig.GetAggregate().Percentile = proto.Float64(50)
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, percentileConfig))

	tests := []struct {
		name        string
		engine      storepb.Engine
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:        "missing config",
			engine:      storepb.Engine_POSTGRES,
			config:      &storepb.QualityRuleConfig{},
			wantMessage: "empty",
		},
		{
			name:        "unknown column",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("nope", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil),
			wantMessage: "does not exist",
		},
		{
			name:        "unspecified function",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED, proto.Float64(0), nil),
			wantMessage: "aggregate function is required",
		},
		{
			name:        "percentile without percentile",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, proto.Float64(0), nil),
			wantMessage: "needs a percentile",
		},
		{
			name:   "percentile above one hundred",
			engine: storepb.Engine_POSTGRES,
			config: func() *storepb.QualityRuleConfig {
				config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, proto.Float64(0), nil)
				config.GetAggregate().Percentile = proto.Float64(101)
				return config
			}(),
			wantMessage: "0–100",
		},
		{
			name:   "percentile on a non-percentile function",
			engine: storepb.Engine_POSTGRES,
			config: func() *storepb.QualityRuleConfig {
				config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil)
				config.GetAggregate().Percentile = proto.Float64(50)
				return config
			}(),
			wantMessage: "only used by the PERCENTILE",
		},
		{
			name:        "sum on a string column",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("status", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil),
			wantMessage: "needs a numeric column",
		},
		{
			name:        "avg on a money column",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("money_amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, proto.Float64(0), nil),
			wantMessage: "not defined for money columns",
		},
		{
			name:        "min on a temporal column",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("created_at", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, proto.Float64(0), nil),
			wantMessage: "needs a numeric column",
		},
		{
			name:        "min on a string column",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("status", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, proto.Float64(0), nil),
			wantMessage: "needs a numeric column",
		},
		{
			name:        "no bounds",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, nil, nil),
			wantMessage: "needs min or max",
		},
		{
			name:        "min above max",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(10), proto.Float64(1)),
			wantMessage: "exceeds max",
		},
		{
			name:   "warn min below min",
			engine: storepb.Engine_POSTGRES,
			config: func() *storepb.QualityRuleConfig {
				config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(10), nil)
				config.GetAggregate().WarnMin = proto.Float64(5)
				return config
			}(),
			wantMessage: "below min",
		},
		{
			name:   "warn max above max",
			engine: storepb.Engine_POSTGRES,
			config: func() *storepb.QualityRuleConfig {
				config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, nil, proto.Float64(100))
				config.GetAggregate().WarnMax = proto.Float64(150)
				return config
			}(),
			wantMessage: "above max",
		},
		{
			name:        "non-finite bound",
			engine:      storepb.Engine_POSTGRES,
			config:      aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(math.NaN()), nil),
			wantMessage: "finite",
		},
		{
			name:   "percent metric",
			engine: storepb.Engine_POSTGRES,
			config: func() *storepb.QualityRuleConfig {
				config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil)
				config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
				return config
			}(),
			wantMessage: "absolute values",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target := compileTestTarget(test.engine)
			target.Columns = append(target.Columns, ColumnInfo{Name: "money_amount", Type: "money", Kind: ColumnKindNumeric})
			err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestAggregateSupport pins the capability table: the rule runs on every dialect
// (PERCENTILE is restricted per engine at validation time) and only on tables.
func TestAggregateSupport(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE)
		require.True(t, ok, reason)
	}
	require.True(t, RuleTypeForTarget(storepb.MetaType_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE))
	require.False(t, RuleTypeForTarget(storepb.MetaType_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE))

	ok, _ := aggregateFunctionSupported(storepb.Engine_MYSQL, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE)
	require.False(t, ok)
	ok, _ = aggregateFunctionSupported(storepb.Engine_POSTGRES, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE)
	require.True(t, ok)
	ok, _ = aggregateFunctionSupported(storepb.Engine_MSSQL, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV)
	require.True(t, ok)
}

// TestAggregateExecutionRejectsPercentMetric pins the runtime backstop: an
// aggregate threshold is an absolute value, so a PERCENT metric is refused before
// any query runs.
func TestAggregateExecutionRejectsPercentMetric(t *testing.T) {
	t.Parallel()

	config := aggregateConfig("amount", storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(0), nil)
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	_, err := NewExecutor().Execute(context.Background(), nil, execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, config), ExecOptions{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "absolute values")
}
