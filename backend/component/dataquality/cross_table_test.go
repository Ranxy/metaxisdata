package dataquality

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// crossReferenceGUID is the registry GUID of the reference relation the golden
// rules compare against: `customers` in the target's own database.
const crossReferenceGUID = "instances/ins1;shop;;customers"

// crossReferenceTarget builds the reference relation one test reads. The instance
// and database are parameters because the connection-sharing decision turns on
// them: the same database shares the target's statement on every engine, another
// database shares it only on the MySQL family, and another instance never does.
func crossReferenceTarget(engine storepb.Engine, instance, database string) ResolvedTarget {
	schema := ""
	if engine == storepb.Engine_POSTGRES || engine == storepb.Engine_MSSQL {
		schema = "public"
	}
	return ResolvedTarget{
		Target: Target{
			GUID:     common.BuildMetaGUID(instance, database, schema, "customers"),
			MetaType: storepb.MetaType_TABLE,
			Path: &storepb.QualityTargetPath{
				Instance: instance,
				Database: database,
				Schema:   schema,
				Object:   "customers",
			},
		},
		Engine:             engine,
		InstanceResourceID: instance,
		Database:           database,
		Schema:             schema,
		Table:              "customers",
		Columns: []ColumnInfo{
			{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
			{Name: "total", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
			{Name: "status", Type: "varchar(32)", Kind: ColumnKindString},
			{Name: "created_at", Type: "timestamp", Kind: ColumnKindTemporal},
		},
		KeyColumns: []string{"id"},
	}
}

// crossReferenceConfig names the reference relation in a rule config. The compiler
// and the executor read the resolved target; the config carries the identity the
// save path resolves and the editor displays.
func crossReferenceConfig(guid string) *storepb.CrossTableReference {
	return &storepb.CrossTableReference{MetaGuid: guid, MetaType: storepb.MetaType_TABLE}
}

// crossTableConfig builds a reconciliation rule config against one reference.
func crossTableConfig(referenceGUID string, left storepb.QualityAggregateFunction, leftColumn string,
	right storepb.QualityAggregateFunction, rightColumn string, tolerance *float64) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{
		CrossTable: &storepb.CrossTableConfig{
			Reference:     crossReferenceConfig(referenceGUID),
			LeftFunction:  left,
			LeftColumn:    leftColumn,
			RightFunction: right,
			RightColumn:   rightColumn,
			Tolerance:     tolerance,
		},
	}}
}

// withRightFilter adds the reference-side filter to a reconciliation config.
func withRightFilter(config *storepb.QualityRuleConfig, clauses ...*storepb.QualityFilterClause) *storepb.QualityRuleConfig {
	config.GetCrossTable().RightFilter = &storepb.QualityFilter{Clauses: clauses}
	return config
}

// crossCompileInput is the compile input one reconciliation test compiles with.
func crossCompileInput(engine storepb.Engine, config *storepb.QualityRuleConfig, reference ResolvedTarget, filter Filter) CompileInput {
	dialect, err := DialectFor(engine)
	if err != nil {
		panic(err)
	}
	return CompileInput{
		Dialect:   dialect,
		Target:    compileTestTarget(engine),
		Type:      storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		Config:    config,
		Filter:    filter,
		Reference: &reference,
	}
}

// TestCrossTableSQLGoldens pins the shared-connection statement on every dialect
// family: two scalar subqueries in one row, the rule's filter on the target side
// only, and the reference relation resolved from the registry.
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
			config: crossTableConfig(crossReferenceGUID,
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
			config: crossTableConfig(crossReferenceGUID,
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
			name: "filter applies to the target side only",
			config: crossTableConfig(crossReferenceGUID,
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
				input := crossCompileInput(engine, test.config,
					crossReferenceTarget(engine, "instances/ins1", "shop"), test.filter)
				query, err := Compile(input)
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

// TestCrossTableReferenceFilter pins the reference-side filter: it bounds the
// reference side only, and in a shared statement its placeholders continue after
// the target side's arguments instead of restarting at 1.
func TestCrossTableReferenceFilter(t *testing.T) {
	t.Parallel()

	leftFilter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	config := withRightFilter(crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
		&storepb.QualityFilterClause{
			Column: "status",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"active"},
		},
	)

	tests := []struct {
		name     string
		engine   storepb.Engine
		wantSQL  string
		wantArgs []any
	}{
		{
			name:   "mysql positional args follow the statement order",
			engine: storepb.Engine_MYSQL,
			wantSQL: "SELECT (SELECT COUNT(*) FROM `shop`.`orders` WHERE `status` = ?) AS left_value, " +
				"(SELECT COUNT(*) FROM `shop`.`customers` WHERE `status` = ?) AS right_value",
			wantArgs: []any{"paid", "active"},
		},
		{
			name:   "postgres numbered args continue past the target",
			engine: storepb.Engine_POSTGRES,
			wantSQL: `SELECT (SELECT COUNT(*) FROM "public"."orders" WHERE "status" = $1) AS left_value, ` +
				`(SELECT COUNT(*) FROM "public"."customers" WHERE "status" = $2) AS right_value`,
			wantArgs: []any{"paid", "active"},
		},
		{
			name:   "mssql numbered args continue past the target",
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT (SELECT COUNT(*) FROM [public].[orders] WHERE [status] = @p1) AS left_value, " +
				"(SELECT COUNT(*) FROM [public].[customers] WHERE [status] = @p2) AS right_value",
			wantArgs: []any{"paid", "active"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := crossCompileInput(test.engine, config,
				crossReferenceTarget(test.engine, "instances/ins1", "shop"), leftFilter)
			query, err := Compile(input)
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
		})
	}
}

// TestCrossTableReferenceFilterAfterContractFilter pins the argument order when the
// target side already binds the rule's filter and the target contract's filter: the
// reference-side placeholders must continue after both, not restart at 1.
func TestCrossTableReferenceFilterAfterContractFilter(t *testing.T) {
	t.Parallel()

	reference := crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "shop")
	config := withRightFilter(crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
		&storepb.QualityFilterClause{
			Column: "status",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"active"},
		},
	)
	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		Config:  config,
		Filter: Filter{Clauses: []FilterClause{
			{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		}},
		ContractFilter: Filter{Clauses: []FilterClause{
			{Column: "amount", Op: FilterGT, Values: []string{"10"}},
		}},
		Reference: &reference,
	})
	require.NoError(t, err)
	require.Equal(t,
		`SELECT (SELECT COUNT(*) FROM "public"."orders" WHERE "status" = $1 AND "amount" > $2) AS left_value, `+
			`(SELECT COUNT(*) FROM "public"."customers" WHERE "status" = $3) AS right_value`,
		query.SQL)
	require.Equal(t, []any{"paid", int64(10), "active"}, query.Args)
}

// TestCrossTableSeparateConnection pins the two-statement form: the reference is
// read over its own connection, so the rule's statement carries the target side
// only and the reference statement carries its own filter with its own numbering.
func TestCrossTableSeparateConnection(t *testing.T) {
	t.Parallel()

	config := withRightFilter(crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "total", nil),
		&storepb.QualityFilterClause{
			Column: "status",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"active"},
		},
	)
	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}

	t.Run("postgres other database", func(t *testing.T) {
		t.Parallel()
		reference := crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "warehouse")
		input := crossCompileInput(storepb.Engine_POSTGRES, config, reference, filter)
		left, err := Compile(input)
		require.NoError(t, err)
		require.Equal(t, `SELECT SUM("amount") AS left_value FROM "public"."orders" WHERE "status" = $1`, left.SQL)
		require.Equal(t, []any{"paid"}, left.Args)
		require.Equal(t, []string{"left_value"}, left.Columns)

		right, err := CompileReference(input)
		require.NoError(t, err)
		require.Equal(t, `SELECT SUM("total") AS right_value FROM "public"."customers" WHERE "status" = $1`, right.SQL)
		require.Equal(t, []any{"active"}, right.Args)
		require.Equal(t, []string{"right_value"}, right.Columns)
	})

	t.Run("other instance", func(t *testing.T) {
		t.Parallel()
		reference := crossReferenceTarget(storepb.Engine_MYSQL, "instances/ins2", "shop")
		input := crossCompileInput(storepb.Engine_MYSQL, config, reference, filter)
		left, err := Compile(input)
		require.NoError(t, err)
		require.Equal(t, "SELECT SUM(`amount`) AS left_value FROM `shop`.`orders` WHERE `status` = ?", left.SQL)

		right, err := CompileReference(input)
		require.NoError(t, err)
		require.Equal(t, "SELECT SUM(`total`) AS right_value FROM `shop`.`customers` WHERE `status` = ?", right.SQL)
		require.Equal(t, []any{"active"}, right.Args)
	})
}

// TestReferenceSharesConnection pins the rule that decides one statement from two:
// the same instance, and either the same database or a family that qualifies the
// database in the table name.
func TestReferenceSharesConnection(t *testing.T) {
	t.Parallel()

	mysqlTarget := compileTestTarget(storepb.Engine_MYSQL)
	pgTarget := compileTestTarget(storepb.Engine_POSTGRES)

	require.True(t, ReferenceSharesConnection(mysqlTarget,
		crossReferenceTarget(storepb.Engine_MYSQL, "instances/ins1", "shop")))
	require.True(t, ReferenceSharesConnection(mysqlTarget,
		crossReferenceTarget(storepb.Engine_MYSQL, "instances/ins1", "warehouse")),
		"the MySQL family qualifies the database on one connection")

	require.True(t, ReferenceSharesConnection(pgTarget,
		crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "shop")))
	require.False(t, ReferenceSharesConnection(pgTarget,
		crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "warehouse")),
		"PostgreSQL selects the database on the connection")

	require.False(t, ReferenceSharesConnection(mysqlTarget,
		crossReferenceTarget(storepb.Engine_MYSQL, "instances/ins2", "shop")))
	require.False(t, ReferenceSharesConnection(pgTarget,
		crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins2", "shop")))
}

// TestCompileCrossTableReferenceRefusals pins the compile-time refusals that need
// the reference's own shape: a missing reference and a right column the reference
// does not have.
func TestCompileCrossTableReferenceRefusals(t *testing.T) {
	t.Parallel()

	reference := crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "shop")
	tests := []struct {
		name        string
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name: "no reference",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "needs a reference table",
		},
		{
			name: "right column does not exist",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "nope", nil),
			wantMessage: `column "nope" does not exist`,
		},
		{
			name: "right filter names a column the reference does not have",
			config: withRightFilter(crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
				&storepb.QualityFilterClause{
					Column: "nope",
					Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
					Values: []string{"active"},
				}),
			wantMessage: `filter column "nope" does not exist`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := crossCompileInput(storepb.Engine_POSTGRES, test.config, reference, Filter{})
			if test.name == "no reference" {
				input.Reference = nil
			}
			_, err := Compile(input)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestValidateCrossTableReference pins the registry-backed reference checks: the
// right column must exist and every non-COUNT side must be numeric, because the
// evaluator subtracts the two observations as numbers.
func TestValidateCrossTableReference(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	reference := crossReferenceTarget(storepb.Engine_POSTGRES, "instances/ins1", "shop")
	tests := []struct {
		name        string
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name: "count against the reference row count",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
		},
		{
			name: "numeric aggregate on both sides",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "total", nil),
		},
		{
			name: "right column does not exist",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "nope", nil),
			wantMessage: `right column "nope" does not exist`,
		},
		{
			name: "right aggregate on a timestamp",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, "created_at", nil),
			wantMessage: "needs a numeric column",
		},
		{
			name: "left aggregate on a timestamp",
			config: crossTableConfig(crossReferenceGUID,
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, "created_at",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "needs a numeric column",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateCrossTableReference(target, reference, test.config)
			if test.wantMessage == "" {
				require.NoError(t, err)
				return
			}
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestEvaluateCrossTable pins the tolerance judgement: the tolerance is the GT
// threshold on the absolute difference, so a difference equal to it passes.
func TestEvaluateCrossTable(t *testing.T) {
	t.Parallel()

	exact := crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
	exactRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, Config: exact}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, Evaluate(exactRule, Observation{Observed: 0}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(exactRule, Observation{Observed: 0.5}, testNow).GetStatus())

	tolerated := crossTableConfig(crossReferenceGUID,
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
	thresholded := crossTableConfig(crossReferenceGUID,
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

// crossExecReference is the reference execution one executor test hands in, on a
// PostgreSQL reference. A shared connection leaves Open unused: the executor must
// never call it.
func crossExecReference(instance, database string, open func(context.Context) (db.ReadOnlyQueryer, func(), error)) *ReferenceExecution {
	return &ReferenceExecution{
		Resolution: &Resolution{Target: crossReferenceTarget(storepb.Engine_POSTGRES, instance, database)},
		Open:       open,
	}
}

// TestExecuteCrossTableSharedConnection pins the one-statement path: a single
// query, the absolute difference as a NUMBER, and a NULL on either side as
// ObservedNull.
func TestExecuteCrossTableSharedConnection(t *testing.T) {
	t.Parallel()

	config := crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "total", proto.Float64(1))

	openCalls := 0
	reference := crossExecReference("instances/ins1", "shop",
		func(context.Context) (db.ReadOnlyQueryer, func(), error) {
			openCalls++
			return nil, nil, errors.New("a shared connection must not open a reference session")
		})

	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, int64(100), int64(90)),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
		ExecOptions{Reference: reference})
	require.NoError(t, err)
	require.Equal(t, 10.0, execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
	require.False(t, execution.Observation.ObservedNull)
	require.Empty(t, execution.ReferenceExecutedSQL)
	require.Zero(t, openCalls, "a shared connection needs no second session")
	require.Len(t, queryer.queries, 1)

	// Both sides are numeric, but SQL SUM can normalize to float64.
	queryer = &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, 100.5, 90.0),
	}}
	execution, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
		ExecOptions{Reference: reference})
	require.NoError(t, err)
	require.InDelta(t, 10.5, execution.Observation.Observed, 1e-9)

	queryer = &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"left_value", "right_value"}, nil, int64(90)),
	}}
	execution, err = NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
		ExecOptions{Reference: reference})
	require.NoError(t, err)
	require.True(t, execution.Observation.ObservedNull)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER, execution.Observation.ObservedKind)
}

// TestExecuteCrossTableSeparateConnection pins the two-statement path: the target
// side runs on the caller's session, the reference side on the session the
// reference opener supplies, and both statements are recorded.
func TestExecuteCrossTableSeparateConnection(t *testing.T) {
	t.Parallel()

	config := crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)

	newReference := func(referenceQueryer *execFakeQueryer, released *int) *ReferenceExecution {
		return crossExecReference("instances/ins1", "warehouse",
			func(context.Context) (db.ReadOnlyQueryer, func(), error) {
				return referenceQueryer, func() { *released++ }, nil
			})
	}

	t.Run("both sides run and both statements are recorded", func(t *testing.T) {
		t.Parallel()
		targetQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"left_value"}, int64(100)),
		}}
		referenceQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"right_value"}, int64(95)),
		}}
		released := 0
		execution, err := NewExecutor().Execute(context.Background(), targetQueryer,
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{Reference: newReference(referenceQueryer, &released)})
		require.NoError(t, err)
		require.Equal(t, 5.0, execution.Observation.Observed)
		require.Equal(t, `SELECT COUNT(*) AS left_value FROM "public"."orders"`, execution.ExecutedSQL)
		require.Equal(t, `SELECT COUNT(*) AS right_value FROM "public"."customers"`, execution.ReferenceExecutedSQL)
		require.Equal(t, 1, released, "the reference session is released")
		require.Len(t, targetQueryer.queries, 1)
		require.Len(t, referenceQueryer.queries, 1)
	})

	t.Run("a NULL reference side is missing data", func(t *testing.T) {
		t.Parallel()
		targetQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"left_value"}, int64(100)),
		}}
		referenceQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"right_value"}, nil),
		}}
		released := 0
		execution, err := NewExecutor().Execute(context.Background(), targetQueryer,
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{Reference: newReference(referenceQueryer, &released)})
		require.NoError(t, err)
		require.True(t, execution.Observation.ObservedNull)
		require.Equal(t, 1, released)
	})

	t.Run("an opener error fails the execution before the target scan", func(t *testing.T) {
		t.Parallel()
		targetQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"left_value"}, int64(100)),
		}}
		wantErr := errors.New("no read-only data source")
		// Another instance always needs its own session, whatever its database is.
		reference := crossExecReference("instances/ins2", "shop",
			func(context.Context) (db.ReadOnlyQueryer, func(), error) {
				return nil, nil, wantErr
			})
		_, err := NewExecutor().Execute(context.Background(), targetQueryer,
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{Reference: reference})
		require.ErrorIs(t, err, wantErr)
		require.Empty(t, targetQueryer.queries, "a configuration failure must not scan the target")
	})

	t.Run("a reference query error fails the execution", func(t *testing.T) {
		t.Parallel()
		targetQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"left_value"}, int64(100)),
		}}
		referenceQueryer := &execFakeQueryer{errs: []error{errors.New("reference timeout")}}
		released := 0
		_, err := NewExecutor().Execute(context.Background(), targetQueryer,
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{Reference: newReference(referenceQueryer, &released)})
		require.Error(t, err)
		require.Equal(t, 1, released, "a failed reference query still releases the session")
	})

	t.Run("either side truncated marks the execution truncated", func(t *testing.T) {
		t.Parallel()
		left := aggregateRow([]string{"left_value"}, int64(100))
		left.Truncated = true
		targetQueryer := &execFakeQueryer{results: []*db.QueryResult{left}}
		referenceQueryer := &execFakeQueryer{results: []*db.QueryResult{
			aggregateRow([]string{"right_value"}, int64(95)),
		}}
		released := 0
		execution, err := NewExecutor().Execute(context.Background(), targetQueryer,
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{Reference: newReference(referenceQueryer, &released)})
		require.NoError(t, err)
		require.True(t, execution.Truncated, "a truncated target side marks the whole execution")
		require.True(t, execution.Observation.Truncated)
	})

	t.Run("a reconciliation without a reference is an invalid rule", func(t *testing.T) {
		t.Parallel()
		_, err := NewExecutor().Execute(context.Background(), &execFakeQueryer{},
			execTestResolution(storepb.Engine_POSTGRES),
			execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
			ExecOptions{})
		require.Equal(t, common.Invalid, common.ErrorCode(err))
		require.Contains(t, err.Error(), "needs its reference table to be resolved")
	})
}

// TestValidateCrossTableConfig pins every target-free save-time refusal of a
// reconciliation rule.
func TestValidateCrossTableConfig(t *testing.T) {
	t.Parallel()

	pgTarget := compileTestTarget(storepb.Engine_POSTGRES)
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)))
	require.NoError(t, ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		withRightFilter(crossTableConfig(crossReferenceGUID,
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "amount",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG, "total", proto.Float64(1)),
			&storepb.QualityFilterClause{
				Column: "status",
				Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
				Values: []string{"active"},
			})))
	// The evaluator subtracts the two observations as numbers, so a temporal MIN/MAX
	// is not a difference and is refused even against a COUNT right side.
	err := ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN, "created_at",
			storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil))
	require.Error(t, err)
	require.Contains(t, err.Error(), "needs a numeric column")

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
			name: "missing reference",
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig(crossReferenceGUID,
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
				config.GetCrossTable().Reference = nil
				return config
			}(),
			wantMessage: "needs a reference table",
		},
		{
			name: "reference without a GUID",
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig(" ",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
				return config
			}(),
			wantMessage: "needs a reference table",
		},
		{
			name: "reference without an object type",
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig(crossReferenceGUID,
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
				config.GetCrossTable().Reference.MetaType = storepb.MetaType_UNSPECIFIED
				return config
			}(),
			wantMessage: "needs an object type",
		},
		{
			name: "reference is not a relation",
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig(crossReferenceGUID,
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
				config.GetCrossTable().Reference.MetaType = storepb.MetaType_COLUMN
				return config
			}(),
			wantMessage: "must be a table, external table, view or materialized view",
		},
		{
			name: "unspecified left function",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "cross-table function is required",
		},
		{
			name: "unsupported right function",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_STDDEV, "amount", nil),
			wantMessage: "cross_table supports COUNT, SUM, AVG, MIN and MAX only",
		},
		{
			name: "count with a left column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "amount",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "takes no left_column",
		},
		{
			name: "count with a right column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "total", nil),
			wantMessage: "takes no right_column",
		},
		{
			name: "sum without a left column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "column is required",
		},
		{
			name: "unknown left column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "nope",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "does not exist",
		},
		{
			name: "sum on a string left column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "status",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil),
			wantMessage: "needs a numeric column",
		},
		{
			name: "non-count without a right column",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, "", nil),
			wantMessage: "needs a right_column",
		},
		{
			name: "negative tolerance",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(-1)),
			wantMessage: "non-negative",
		},
		{
			name: "non-finite tolerance",
			config: crossTableConfig(crossReferenceGUID, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
				storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", proto.Float64(math.NaN())),
			wantMessage: "non-negative",
		},
		{
			name: "percent metric",
			config: func() *storepb.QualityRuleConfig {
				config := crossTableConfig(crossReferenceGUID,
					storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
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
			err := ValidateConfig(pgTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestValidateCrossTableRightFilter pins the reference-side filter's own bounds: it
// is a filter like any other, so clause count and IN-list size are enforced before
// the rule reaches the registry.
func TestValidateCrossTableRightFilter(t *testing.T) {
	t.Parallel()

	clauses := make([]*storepb.QualityFilterClause, 0, MaxFilterClauses+1)
	for index := 0; index <= MaxFilterClauses; index++ {
		clauses = append(clauses, &storepb.QualityFilterClause{
			Column: "status",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"active"},
		})
	}
	config := crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
	config.GetCrossTable().RightFilter = &storepb.QualityFilter{Clauses: clauses}
	err := ValidateConfig(compileTestTarget(storepb.Engine_POSTGRES),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "at most")
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

// TestCrossTableReferenceMetaTypes pins which registry objects may be a reference:
// a view or an external table may, because the reference side is only read; the
// rule's own target keeps its table-only restriction (above).
func TestCrossTableReferenceMetaTypes(t *testing.T) {
	t.Parallel()

	for _, metaType := range []storepb.MetaType{
		storepb.MetaType_TABLE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_VIEW,
		storepb.MetaType_MATERIALIZED_VIEW,
	} {
		require.NoError(t, validateReferenceMetaType(metaType), metaType.String())
	}
	for _, metaType := range []storepb.MetaType{
		storepb.MetaType_UNSPECIFIED,
		storepb.MetaType_COLUMN,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_DATABASE,
		storepb.MetaType_INSTANCE,
	} {
		require.Error(t, validateReferenceMetaType(metaType), metaType.String())
	}
}

// TestCrossTableExecutionRejectsPercentMetric pins the runtime backstop: a
// tolerance is an absolute value, so a PERCENT metric is refused before any query
// runs.
func TestCrossTableExecutionRejectsPercentMetric(t *testing.T) {
	t.Parallel()

	config := crossTableConfig(crossReferenceGUID,
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "",
		storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, "", nil)
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	_, err := NewExecutor().Execute(context.Background(), nil, execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, config),
		ExecOptions{Reference: crossExecReference("instances/ins1", "shop", nil)})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "absolute values")
}
