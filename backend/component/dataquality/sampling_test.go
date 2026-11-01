package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

func maxRowSampling(rows int64) *storepb.QualityRuleSampling {
	return &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: rows}}
}

func percentSampling(percent float64) *storepb.QualityRuleSampling {
	return &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_Percent{Percent: percent}}
}

// The two methods render to the engine's own syntax; the engine without a table
// sampling clause refuses the method instead of approximating it.
func TestDialectSamplingRelations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engine      storepb.Engine
		table       string
		maxRows     string
		percent     string
		wantSample  bool
		percentErr  string
		boundedErr  string
		description string
	}{
		{
			engine:     storepb.Engine_MYSQL,
			table:      "`shop`.`orders`",
			maxRows:    "(SELECT * FROM `shop`.`orders` LIMIT 1000) dq",
			wantSample: false,
			percentErr: "has no table sampling clause",
		},
		{
			engine:     storepb.Engine_POSTGRES,
			table:      `"public"."orders"`,
			maxRows:    `(SELECT * FROM "public"."orders" LIMIT 1000) dq`,
			percent:    `"public"."orders" TABLESAMPLE SYSTEM (10.5)`,
			wantSample: true,
		},
		{
			engine:     storepb.Engine_MSSQL,
			table:      "[public].[orders]",
			maxRows:    "(SELECT TOP (1000) * FROM [public].[orders]) dq",
			percent:    "[public].[orders] TABLESAMPLE (10.5 PERCENT)",
			wantSample: true,
		},
		{
			engine:     storepb.Engine_DORIS,
			table:      "`shop`.`orders`",
			maxRows:    "(SELECT * FROM `shop`.`orders` LIMIT 1000) dq",
			percent:    "`shop`.`orders` TABLESAMPLE (10.5 PERCENT)",
			wantSample: true,
		},
		{
			engine:     storepb.Engine_STARROCKS,
			table:      "`shop`.`orders`",
			maxRows:    "(SELECT * FROM `shop`.`orders` LIMIT 1000) dq",
			wantSample: false,
			percentErr: "has no table sampling clause",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			require.Equal(t, test.wantSample, dialect.SupportsTableSample())

			bounded, err := dialect.BoundedRelation(test.table, "", 1000)
			require.NoError(t, err)
			require.Equal(t, test.maxRows, bounded)

			sampled, err := dialect.SampledRelation(test.table, 10.5)
			if test.percentErr != "" {
				require.ErrorContains(t, err, test.percentErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.percent, sampled)
			}
		})
	}
}

func TestBoundedRelationRefusesANonPositiveLimit(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_MSSQL} {
		dialect, err := DialectFor(engine)
		require.NoError(t, err)
		_, err = dialect.BoundedRelation("t", "", 0)
		require.Error(t, err)
	}
}

func TestValidateSampling(t *testing.T) {
	t.Parallel()

	mysql := compileTestTarget(storepb.Engine_MYSQL)
	postgres := compileTestTarget(storepb.Engine_POSTGRES)
	doris := compileTestTarget(storepb.Engine_DORIS)
	view := compileTestTarget(storepb.Engine_POSTGRES)
	view.MetaType = storepb.MetaType_VIEW

	tests := []struct {
		name     string
		target   ResolvedTarget
		ruleType storepb.QualityRuleType
		sampling *storepb.QualityRuleSampling
		wantErr  string
	}{
		{
			name:     "no sampling is fine",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			sampling: nil,
		},
		{
			name:     "row limit",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			sampling: maxRowSampling(1000),
		},
		{
			name:     "row limit of zero",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			sampling: maxRowSampling(0),
			wantErr:  "must be between 1 and 10000000",
		},
		{
			name:     "row limit above the cap",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			sampling: maxRowSampling(maxSampledRows + 1),
			wantErr:  "must be between 1 and 10000000",
		},
		{
			name:     "percent on a table it can express",
			target:   postgres,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(10),
		},
		{
			name:     "percent below 0",
			target:   postgres,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(-1),
			wantErr:  "greater than 0 and less than 100",
		},
		{
			name:     "percent at 100",
			target:   postgres,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(100),
			wantErr:  "greater than 0 and less than 100",
		},
		{
			name:     "percent on an engine without a sampling clause",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(10),
			wantErr:  "cannot sample by percentage; use a row limit instead",
		},
		{
			name:     "percent on a view",
			target:   view,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(10),
			wantErr:  "applies to tables only",
		},
		{
			name:     "percent on Doris",
			target:   doris,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			sampling: percentSampling(10),
		},
		{
			name:     "row count cannot be sampled",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			sampling: maxRowSampling(1000),
			wantErr:  "not the table's row count",
		},
		{
			name:     "freshness cannot be sampled",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			sampling: maxRowSampling(1000),
			wantErr:  "can miss the newest row",
		},
		{
			name:     "custom SQL cannot be sampled",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			sampling: maxRowSampling(1000),
			wantErr:  "never rewritten",
		},
		{
			name:     "CEL keeps its own sample setting",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
			sampling: maxRowSampling(1000),
			wantErr:  "own sample_rows setting",
		},
		{
			name:     "referential integrity reads two relations",
			target:   mysql,
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			sampling: maxRowSampling(1000),
			wantErr:  "reads two relations",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateSampling(test.target, test.ruleType, test.sampling)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}
}

// A bounded sample moves the filter inside the derived relation: the limit must
// bound the filtered rows, not an arbitrary slice of the table.
func TestCompileBoundedSample(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config: &storepb.QualityRuleConfig{
			Config:   &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: ptrFloat(5)}},
			Sampling: maxRowSampling(5000),
		},
		Filter: Filter{Clauses: []FilterClause{
			{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `email` IS NULL THEN 1 ELSE 0 END) "+
		"AS failed_rows FROM (SELECT * FROM `shop`.`orders` WHERE `status` = ? LIMIT 5000) dq", query.SQL)
	require.Equal(t, []any{"paid"}, query.Args)
}

// A percentage sample leaves the filter outside the engine's sampling clause.
func TestCompilePercentSample(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		Config: &storepb.QualityRuleConfig{
			Config:   &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{Column: "email", Pattern: "^[^@]+@[^@]+$"}},
			Sampling: percentSampling(10),
		},
		Filter: Filter{Clauses: []FilterClause{
			{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		}},
	})
	require.NoError(t, err)
	// Numbered dialects number the filter's arguments first (compileArgs), so the
	// WHERE value is $1 and the pattern in the select list is $2.
	require.Equal(t, `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "email" IS NULL OR "email" !~ $2 `+
		`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders" TABLESAMPLE SYSTEM (10) WHERE "status" = $1`, query.SQL)
	require.Equal(t, []any{"paid", "^[^@]+@[^@]+$"}, query.Args)
}

// A sampled uniqueness rule judges duplicates and the denominator from one
// sampled relation, which is what the single-pass rewrite made possible.
func TestCompileSampledUnique(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MSSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MSSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Config: &storepb.QualityRuleConfig{
			Config:   &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "email"}},
			Sampling: percentSampling(10),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT COALESCE(SUM(CASE WHEN cnt > 1 AND keyed = 1 THEN cnt - 1 ELSE 0 END), 0) AS failed_rows, "+
		"COUNT(CASE WHEN cnt > 1 AND keyed = 1 THEN 1 END) AS duplicate_groups, COALESCE(SUM(cnt), 0) AS total_rows "+
		"FROM (SELECT [email], COUNT(*) AS cnt, CASE WHEN [email] IS NOT NULL THEN 1 ELSE 0 END AS keyed "+
		"FROM [public].[orders] TABLESAMPLE (10 PERCENT) GROUP BY [email]) dq", query.SQL)
}

// The failed-row sample reads the same sampled relation, so the evidence comes from
// the rows the verdict was judged on and its cost stays bounded too.
func TestCompileSampledFailureSample(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	input := CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		Config: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
				Column: "email", MaxLen: ptrInt64(64),
			}},
			Sampling: maxRowSampling(5000),
		},
	}
	sample, err := CompileFailureRows(input, 20)
	require.NoError(t, err)
	require.Equal(t, "SELECT `id`, `email` FROM (SELECT * FROM `shop`.`orders` LIMIT 5000) dq "+
		"WHERE (CHAR_LENGTH(`email`) > ?) LIMIT 20", sample.SQL)
}

// A stored rule with a sampling method its type forbids cannot compile into a
// statement that silently ignores it.
func TestCompileRefusesSamplingItsTypeForbids(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	_, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config: &storepb.QualityRuleConfig{
			Config:   &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: ptrInt64(1)}},
			Sampling: maxRowSampling(1000),
		},
	})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// A sampled execution records the sample: the flag and the number of rows the
// statement counted, rather than the number of evidence rows.
func TestSampledExecutionMarksObservation(t *testing.T) {
	t.Parallel()

	queryer := &execFakeQueryer{results: []*db.QueryResult{
		aggregateRow([]string{"total_rows", "failed_rows"}, int64(4000), int64(12)),
	}}
	rule := execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, &storepb.QualityRuleConfig{
		Config:   &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullCount: ptrInt64(0)}},
		Sampling: maxRowSampling(4000),
	})
	execution, err := NewExecutor().Execute(t.Context(), queryer, execTestResolution(storepb.Engine_MYSQL), rule, ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Sampled)
	require.True(t, execution.Observation.Sampled)
	require.Equal(t, int64(4000), execution.Observation.SampleRows)
	require.Equal(t, int64(4000), execution.Observation.TotalRows)
	require.Contains(t, queryer.queries[0], "(SELECT * FROM `shop`.`orders` LIMIT 4000) dq")
}

func ptrInt64(value int64) *int64 { return &value }

func ptrFloat(value float64) *float64 { return &value }
