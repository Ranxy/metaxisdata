package dataquality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// groupCheckConfig builds a group-check rule config.
func groupCheckConfig(columns []string, minRows, maxRows *int64) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{
		GroupCheck: &storepb.GroupCheckConfig{
			Columns:         columns,
			MinRowsPerGroup: minRows,
			MaxRowsPerGroup: maxRows,
		},
	}}
}

// TestGroupCheckSQLGoldens pins the aggregate SQL, arguments, columns and kind on
// every dialect family. The bounds sit in the SELECT list and the filter in the
// derived table: numbered dialects bind the filter first and number the bounds
// after it, while a positional dialect binds in text order (bounds first).
func TestGroupCheckSQLGoldens(t *testing.T) {
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
		wantArgs map[storepb.Engine][]any
	}{
		{
			name:   "two bounds one column",
			config: groupCheckConfig([]string{"status"}, int64Ptr(1), int64Ptr(10)),
			mysqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < ? OR cnt > ? THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT `status`, COUNT(*) AS cnt FROM `shop`.`orders` GROUP BY `status`) dq",
			pgSQL: `SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < $1 OR cnt > $2 THEN 1 ELSE 0 END) ` +
				`AS failed_groups FROM (SELECT "status", COUNT(*) AS cnt FROM "public"."orders" GROUP BY "status") dq`,
			mssqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < @p1 OR cnt > @p2 THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT [status], COUNT(*) AS cnt FROM [public].[orders] GROUP BY [status]) dq",
			wantArgs: map[storepb.Engine][]any{
				storepb.Engine_MYSQL:     {int64(1), int64(10)},
				storepb.Engine_STARROCKS: {int64(1), int64(10)},
				storepb.Engine_POSTGRES:  {int64(1), int64(10)},
				storepb.Engine_MSSQL:     {int64(1), int64(10)},
			},
		},
		{
			name:     "min only",
			config:   groupCheckConfig([]string{"status"}, int64Ptr(3), nil),
			mysqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < ? THEN 1 ELSE 0 END) AS failed_groups FROM (SELECT `status`, COUNT(*) AS cnt FROM `shop`.`orders` GROUP BY `status`) dq",
			pgSQL: `SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < $1 THEN 1 ELSE 0 END) AS failed_groups ` +
				`FROM (SELECT "status", COUNT(*) AS cnt FROM "public"."orders" GROUP BY "status") dq`,
			mssqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < @p1 THEN 1 ELSE 0 END) AS failed_groups " +
				"FROM (SELECT [status], COUNT(*) AS cnt FROM [public].[orders] GROUP BY [status]) dq",
			wantArgs: map[storepb.Engine][]any{
				storepb.Engine_MYSQL:     {int64(3)},
				storepb.Engine_STARROCKS: {int64(3)},
				storepb.Engine_POSTGRES:  {int64(3)},
				storepb.Engine_MSSQL:     {int64(3)},
			},
		},
		{
			name:     "max only",
			config:   groupCheckConfig([]string{"status"}, nil, int64Ptr(100)),
			mysqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt > ? THEN 1 ELSE 0 END) AS failed_groups FROM (SELECT `status`, COUNT(*) AS cnt FROM `shop`.`orders` GROUP BY `status`) dq",
			pgSQL: `SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt > $1 THEN 1 ELSE 0 END) AS failed_groups ` +
				`FROM (SELECT "status", COUNT(*) AS cnt FROM "public"."orders" GROUP BY "status") dq`,
			mssqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt > @p1 THEN 1 ELSE 0 END) AS failed_groups " +
				"FROM (SELECT [status], COUNT(*) AS cnt FROM [public].[orders] GROUP BY [status]) dq",
			wantArgs: map[storepb.Engine][]any{
				storepb.Engine_MYSQL:     {int64(100)},
				storepb.Engine_STARROCKS: {int64(100)},
				storepb.Engine_POSTGRES:  {int64(100)},
				storepb.Engine_MSSQL:     {int64(100)},
			},
		},
		{
			name:   "two group columns",
			config: groupCheckConfig([]string{"amount", "status"}, int64Ptr(1), int64Ptr(5)),
			mysqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < ? OR cnt > ? THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT `amount`, `status`, COUNT(*) AS cnt FROM `shop`.`orders` " +
				"GROUP BY `amount`, `status`) dq",
			pgSQL: `SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < $1 OR cnt > $2 THEN 1 ELSE 0 END) ` +
				`AS failed_groups FROM (SELECT "amount", "status", COUNT(*) AS cnt FROM "public"."orders" ` +
				`GROUP BY "amount", "status") dq`,
			mssqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < @p1 OR cnt > @p2 THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT [amount], [status], COUNT(*) AS cnt FROM [public].[orders] " +
				"GROUP BY [amount], [status]) dq",
			wantArgs: map[storepb.Engine][]any{
				storepb.Engine_MYSQL:     {int64(1), int64(5)},
				storepb.Engine_STARROCKS: {int64(1), int64(5)},
				storepb.Engine_POSTGRES:  {int64(1), int64(5)},
				storepb.Engine_MSSQL:     {int64(1), int64(5)},
			},
		},
		{
			name:   "filter and two bounds",
			config: groupCheckConfig([]string{"status"}, int64Ptr(1), int64Ptr(10)),
			filter: filter,
			mysqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < ? OR cnt > ? THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT `status`, COUNT(*) AS cnt FROM `shop`.`orders` " +
				"WHERE `status` = ? GROUP BY `status`) dq",
			pgSQL: `SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < $2 OR cnt > $3 THEN 1 ELSE 0 END) ` +
				`AS failed_groups FROM (SELECT "status", COUNT(*) AS cnt FROM "public"."orders" ` +
				`WHERE "status" = $1 GROUP BY "status") dq`,
			mssqlSQL: "SELECT COUNT(*) AS total_groups, SUM(CASE WHEN cnt < @p2 OR cnt > @p3 THEN 1 ELSE 0 END) " +
				"AS failed_groups FROM (SELECT [status], COUNT(*) AS cnt FROM [public].[orders] " +
				"WHERE [status] = @p1 GROUP BY [status]) dq",
			wantArgs: map[storepb.Engine][]any{
				// A positional dialect binds in text order: the bounds precede the
				// filter's WHERE clause in the statement.
				storepb.Engine_MYSQL:     {int64(1), int64(10), "paid"},
				storepb.Engine_STARROCKS: {int64(1), int64(10), "paid"},
				// A numbered dialect binds the filter first and numbers the bounds
				// after it, so the argument slice stays filter-first.
				storepb.Engine_POSTGRES: {"paid", int64(1), int64(10)},
				storepb.Engine_MSSQL:    {"paid", int64(1), int64(10)},
			},
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
					Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
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
				require.Equal(t, test.wantArgs[engine], query.Args)
				require.Equal(t, []string{"total_groups", "failed_groups"}, query.Columns)
				require.Equal(t, QueryAggregate, query.Kind)
			})
		}
	}
}

// TestGroupCheckSampleSQL pins the failed-group sample: the grouped statement is a
// derived table so the bound is tested outside HAVING, the filter's arguments bind
// first (they appear first in the text), and T-SQL gets an explicit order plus
// OFFSET/FETCH.
func TestGroupCheckSampleSQL(t *testing.T) {
	t.Parallel()

	config := groupCheckConfig([]string{"status"}, int64Ptr(1), int64Ptr(10))
	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	tests := []struct {
		engine  storepb.Engine
		wantSQL string
	}{
		{
			engine: storepb.Engine_MYSQL,
			wantSQL: "SELECT `status`, group_rows FROM (SELECT `status`, COUNT(*) AS group_rows " +
				"FROM `shop`.`orders` WHERE `status` = ? GROUP BY `status`) dq " +
				"WHERE group_rows < ? OR group_rows > ? ORDER BY group_rows DESC, `status` LIMIT 20",
		},
		{
			engine: storepb.Engine_STARROCKS,
			wantSQL: "SELECT `status`, group_rows FROM (SELECT `status`, COUNT(*) AS group_rows " +
				"FROM `shop`.`orders` WHERE `status` = ? GROUP BY `status`) dq " +
				"WHERE group_rows < ? OR group_rows > ? ORDER BY group_rows DESC, `status` LIMIT 20",
		},
		{
			engine: storepb.Engine_POSTGRES,
			wantSQL: `SELECT "status", group_rows FROM (SELECT "status", COUNT(*) AS group_rows ` +
				`FROM "public"."orders" WHERE "status" = $1 GROUP BY "status") dq ` +
				`WHERE group_rows < $2 OR group_rows > $3 ORDER BY group_rows DESC, "status" LIMIT 20`,
		},
		{
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT [status], group_rows FROM (SELECT [status], COUNT(*) AS group_rows " +
				"FROM [public].[orders] WHERE [status] = @p1 GROUP BY [status]) dq " +
				"WHERE group_rows < @p2 OR group_rows > @p3 ORDER BY group_rows DESC, [status] " +
				"OFFSET 0 ROWS FETCH NEXT 20 ROWS ONLY",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			sample, err := CompileFailureRows(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
				Config:  config,
				Filter:  filter,
			}, 20)
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, sample.SQL)
			require.Equal(t, []any{"paid", int64(1), int64(10)}, sample.Args)
			require.Equal(t, []string{"status", "group_rows"}, sample.Columns)
			require.Equal(t, QuerySample, sample.Kind)
		})
	}
}

// TestExecuteGroupCheckObservation pins the aggregate interpretation: the group
// count is the denominator and the failed-group count the numerator.
func TestExecuteGroupCheckObservation(t *testing.T) {
	t.Parallel()

	config := groupCheckConfig([]string{"status"}, int64Ptr(1), int64Ptr(10))
	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_groups", "failed_groups"}, int64(12), int64(3)),
	}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_POSTGRES),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, config), ExecOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(12), execution.Observation.TotalRows)
	require.Equal(t, int64(3), execution.Observation.FailedRows)
	require.Equal(t, float64(3), execution.Observation.Observed)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, execution.Observation.ObservedKind)
	require.Contains(t, execution.ExecutedSQL, "FROM")
	require.Len(t, queryer.queries, 1)
}

// TestEvaluateGroupCheck pins COUNT and PERCENT judgement plus the empty-group
// behavior.
func TestEvaluateGroupCheck(t *testing.T) {
	t.Parallel()

	countConfig := groupCheckConfig([]string{"status"}, int64Ptr(1), nil)
	countConfig.GetGroupCheck().FailThreshold = proto.Float64(2)
	countConfig.GetGroupCheck().WarnThreshold = proto.Float64(1)
	countRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, Config: countConfig}

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS,
		Evaluate(countRule, Observation{TotalRows: 10, FailedRows: 0}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN,
		Evaluate(countRule, Observation{TotalRows: 10, FailedRows: 2}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Evaluate(countRule, Observation{TotalRows: 10, FailedRows: 3}, testNow).GetStatus())

	percentConfig := groupCheckConfig([]string{"status"}, int64Ptr(1), nil)
	percentConfig.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	percentConfig.GetGroupCheck().FailThreshold = proto.Float64(25)
	percentConfig.GetGroupCheck().WarnThreshold = proto.Float64(10)
	percentRule := RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, Config: percentConfig}

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS,
		Evaluate(percentRule, Observation{TotalRows: 10, FailedRows: 1}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN,
		Evaluate(percentRule, Observation{TotalRows: 10, FailedRows: 2}, testNow).GetStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL,
		Evaluate(percentRule, Observation{TotalRows: 10, FailedRows: 3}, testNow).GetStatus())

	// No groups is empty data: it follows on_empty rather than reading as a pass.
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		Evaluate(countRule, Observation{TotalRows: 0, FailedRows: 0}, testNow).GetStatus())
	passOnEmpty := groupCheckConfig([]string{"status"}, int64Ptr(1), nil)
	passOnEmpty.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS,
		Evaluate(RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, Config: passOnEmpty},
			Observation{}, testNow).GetStatus())
}

// TestValidateGroupCheckConfig pins every save-time refusal of a grouped rule.
func TestValidateGroupCheckConfig(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		groupCheckConfig([]string{"status"}, int64Ptr(1), nil)))

	percentValid := groupCheckConfig([]string{"status"}, int64Ptr(1), int64Ptr(10))
	percentValid.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	percentValid.GetGroupCheck().FailThreshold = proto.Float64(50)
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, percentValid))

	percentAboveHundred := groupCheckConfig([]string{"status"}, int64Ptr(1), nil)
	percentAboveHundred.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	percentAboveHundred.GetGroupCheck().FailThreshold = proto.Float64(101)

	tests := []struct {
		name        string
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:        "no columns",
			config:      groupCheckConfig(nil, int64Ptr(1), nil),
			wantMessage: "at least one column",
		},
		{
			name:        "too many columns",
			config:      groupCheckConfig([]string{"id", "email", "amount", "created_at", "status", "qty"}, int64Ptr(1), nil),
			wantMessage: "at most 5 group columns",
		},
		{
			name:        "unknown column",
			config:      groupCheckConfig([]string{"nope"}, int64Ptr(1), nil),
			wantMessage: "does not exist",
		},
		{
			name:        "duplicate columns",
			config:      groupCheckConfig([]string{"status", "status"}, int64Ptr(1), nil),
			wantMessage: "more than once",
		},
		{
			name:        "no bounds",
			config:      groupCheckConfig([]string{"status"}, nil, nil),
			wantMessage: "needs min_rows_per_group or max_rows_per_group",
		},
		{
			name:        "negative min",
			config:      groupCheckConfig([]string{"status"}, int64Ptr(-1), nil),
			wantMessage: "must not be negative",
		},
		{
			name:        "min above max",
			config:      groupCheckConfig([]string{"status"}, int64Ptr(10), int64Ptr(1)),
			wantMessage: "exceeds max_rows_per_group",
		},
		{
			name:        "percent above one hundred",
			config:      percentAboveHundred,
			wantMessage: "above 100 percent",
		},
		{
			name: "warn above fail",
			config: func() *storepb.QualityRuleConfig {
				config := groupCheckConfig([]string{"status"}, int64Ptr(1), nil)
				config.GetGroupCheck().FailThreshold = proto.Float64(1)
				config.GetGroupCheck().WarnThreshold = proto.Float64(2)
				return config
			}(),
			wantMessage: "exceeds failure threshold",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestGroupCheckSupport pins the capability table: every dialect (MSSQL included)
// can run a group check, but it stays table-only because views have no per-column
// type authority.
func TestGroupCheckSupport(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK)
		require.True(t, ok, reason)
	}
	for _, metaType := range []storepb.MetaType{storepb.MetaType_TABLE, storepb.MetaType_EXTERNAL_TABLE} {
		require.True(t, RuleTypeForTarget(metaType, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK), metaType.String())
	}
	for _, metaType := range []storepb.MetaType{storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW} {
		require.False(t, RuleTypeForTarget(metaType, storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK), metaType.String())
	}
}
